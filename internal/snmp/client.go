package snmp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/gosnmp/gosnmp"
)

// Client es lo que el resto de la app necesita de SNMP. Tiene una
// implementación real (gosnmp) y un Mock para tests.
//
// Un Client no es seguro para usarse desde varias goroutines a la vez:
// cada lectura concurrente debe abrir el suyo.
type Client interface {
	// Get pide los OIDs exactos. Los que no existen vuelven con KindNull.
	Get(ctx context.Context, oids ...string) ([]Var, error)
	// Walk recorre la rama root y devuelve sus valores en orden.
	Walk(ctx context.Context, root string) ([]Var, error)
	Close() error
}

// Params configura una conexión.
type Params struct {
	Target    string        // IP u hostname
	Port      uint16        // 0 = 161
	Community string        // "" = "public"
	Version   string        // "1" o "2c"; "" = "1"
	Timeout   time.Duration // 0 = 2 s
	Retries   int
	LocalIP   string // IP local a la que se enlaza el socket (opcional)
}

// Dialer abre clientes. Existe para poder inyectar mocks en las capas de arriba.
type Dialer func(p Params) (Client, error)

// ErrNoResponse indica que el equipo no contestó (timeout).
var ErrNoResponse = errors.New("sin respuesta SNMP")

// ParseVersion convierte el texto de la configuración en la versión de gosnmp.
func ParseVersion(s string) (gosnmp.SnmpVersion, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "1", "v1":
		return gosnmp.Version1, nil
	case "2c", "v2c", "2":
		return gosnmp.Version2c, nil
	}
	return 0, fmt.Errorf("versión SNMP %q no soportada (usa \"1\" o \"2c\")", s)
}

type gsClient struct {
	g *gosnmp.GoSNMP
}

// Dial abre un cliente real con gosnmp. UDP no "conecta" de verdad: un equipo
// apagado sólo se detecta al primer Get/Walk.
func Dial(p Params) (Client, error) {
	ver, err := ParseVersion(p.Version)
	if err != nil {
		return nil, err
	}
	if p.Port == 0 {
		p.Port = 161
	}
	if p.Community == "" {
		p.Community = "public"
	}
	if p.Timeout <= 0 {
		p.Timeout = 2 * time.Second
	}
	if p.Retries < 0 {
		p.Retries = 0
	}
	g := &gosnmp.GoSNMP{
		Target:             p.Target,
		Port:               p.Port,
		Transport:          "udp",
		Community:          p.Community,
		Version:            ver,
		Timeout:            p.Timeout,
		Retries:            p.Retries,
		ExponentialTimeout: false,
		MaxOids:            gosnmp.MaxOids,
		Context:            context.Background(),
	}
	if p.LocalIP != "" {
		g.LocalAddr = p.LocalIP + ":0"
	}
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("abrir socket SNMP hacia %s: %w", p.Target, err)
	}
	return &gsClient{g: g}, nil
}

func (c *gsClient) Close() error {
	if c.g.Conn == nil {
		return nil
	}
	return c.g.Conn.Close()
}

func (c *gsClient) Get(ctx context.Context, oids ...string) ([]Var, error) {
	c.g.Context = ctx
	pkt, err := c.g.Get(oids)
	if err != nil {
		return nil, wrapErr(c.g.Target, err)
	}
	if pkt.Error != gosnmp.NoError {
		// En v1, un OID inexistente hace fallar toda la petición con noSuchName.
		if pkt.Error == gosnmp.NoSuchName && len(oids) == 1 {
			return []Var{{OID: NormOID(oids[0]), Kind: KindNull}}, nil
		}
		return nil, fmt.Errorf("%s respondió con error SNMP %v", c.g.Target, pkt.Error)
	}
	out := make([]Var, 0, len(pkt.Variables))
	for _, pdu := range pkt.Variables {
		out = append(out, fromPDU(pdu))
	}
	return out, nil
}

func (c *gsClient) Walk(ctx context.Context, root string) ([]Var, error) {
	c.g.Context = ctx
	// Siempre GETNEXT, también en v2c: el DCP-L5650DN (NC-8900h) no contesta
	// GETBULK (timeout), pero sí GETNEXT en v2c. Las tablas son chicas (~300 OIDs).
	pdus, err := c.g.WalkAll(root)
	if err != nil {
		return nil, wrapErr(c.g.Target, err)
	}
	out := make([]Var, 0, len(pdus))
	for _, pdu := range pdus {
		v := fromPDU(pdu)
		if v.Kind == KindNull || !HasPrefix(v.OID, root) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func wrapErr(target string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return fmt.Errorf("%s: %w (¿SNMP deshabilitado, community distinta o firewall?)", target, ErrNoResponse)
	}
	return fmt.Errorf("%s: %w", target, err)
}

func fromPDU(pdu gosnmp.SnmpPDU) Var {
	v := Var{OID: NormOID(pdu.Name)}
	switch pdu.Type {
	case gosnmp.Integer:
		v.Kind = KindInt
		v.Int = gosnmp.ToBigInt(pdu.Value).Int64()
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
		v.Kind = KindUint
		v.Uint = gosnmp.ToBigInt(pdu.Value).Uint64()
	case gosnmp.OctetString:
		v.Kind = KindString
		if b, ok := pdu.Value.([]byte); ok {
			v.Str = octets(b)
		}
	case gosnmp.ObjectIdentifier:
		v.Kind = KindOID
		if s, ok := pdu.Value.(string); ok {
			v.Str = NormOID(s)
		}
	case gosnmp.Null, gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		v.Kind = KindNull
	default:
		v.Kind = KindOther
		v.Str = fmt.Sprint(pdu.Value)
	}
	return v
}

// octets muestra texto si es imprimible y, si no, hexadecimal "AA BB CC".
func octets(b []byte) string {
	s := strings.TrimRight(string(b), "\x00")
	printable := true
	for _, r := range s {
		if r == unicode.ReplacementChar || (!unicode.IsPrint(r) && r != '\r' && r != '\n' && r != '\t') {
			printable = false
			break
		}
	}
	if printable {
		return strings.TrimSpace(s)
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, " ")
}
