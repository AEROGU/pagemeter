package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// Límites de rango admitidos para el barrido.
const (
	PrefijoMin = 20
	PrefijoMax = 30
)

// Interfaz es una interfaz IPv4 activa.
type Interfaz struct {
	Nombre string     // nombre de la conexión ("Wi-Fi", "Ethernet")
	IP     net.IP     // IP local
	Red    *net.IPNet // red con su máscara
}

// Prefijo devuelve la longitud de la máscara.
func (i Interfaz) Prefijo() int { ones, _ := i.Red.Mask.Size(); return ones }

// CIDR devuelve la red en formato a.b.c.d/n.
func (i Interfaz) CIDR() string { return i.Red.String() }

// Interfaces lista las interfaces IPv4 en estado Up, sin loopback ni 169.254.x.x.
func Interfaces() ([]Interfaz, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listar interfaces de red: %w", err)
	}
	var out []Interfaz
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			red := &net.IPNet{IP: ip4.Mask(ipn.Mask), Mask: ipn.Mask}
			out = append(out, Interfaz{Nombre: ifc.Name, IP: ip4, Red: red})
		}
	}
	return out, nil
}

// ParseCIDR interpreta un rango "a.b.c.d/n" (n entre 20 y 30). Acepta una IP de
// host dentro de la red ("192.168.1.57/24" equivale a 192.168.1.0/24).
func ParseCIDR(s string) (*net.IPNet, error) {
	s = strings.TrimSpace(s)
	ip, red, err := net.ParseCIDR(s)
	if err != nil || ip.To4() == nil {
		return nil, fmt.Errorf("rango %q inválido (ejemplo: 192.168.1.0/24)", s)
	}
	ones, bits := red.Mask.Size()
	if bits != 32 || ones < PrefijoMin || ones > PrefijoMax {
		return nil, fmt.Errorf("prefijo /%d fuera de rango (se admite de /%d a /%d)", ones, PrefijoMin, PrefijoMax)
	}
	return red, nil
}

// Hosts devuelve las direcciones de host de la red (sin la de red ni la de broadcast).
func Hosts(red *net.IPNet) ([]net.IP, error) {
	ones, bits := red.Mask.Size()
	if bits != 32 || ones < PrefijoMin || ones > PrefijoMax {
		return nil, fmt.Errorf("prefijo /%d fuera de rango (se admite de /%d a /%d)", ones, PrefijoMin, PrefijoMax)
	}
	base := binary.BigEndian.Uint32(red.IP.To4().Mask(red.Mask))
	n := uint32(1) << (32 - ones)
	out := make([]net.IP, 0, n-2)
	for h := base + 1; h < base+n-1; h++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, h)
		out = append(out, ip)
	}
	return out, nil
}

// Respuesta es un equipo que contestó sysDescr en el barrido.
type Respuesta struct {
	IP       string
	SysDescr string
	Modelo   string // de hrDeviceDescr si lo reporta (SUPOSICIÓN: no comprobado)
}

// OpcionesBarrido configura Barrer.
type OpcionesBarrido struct {
	Base         snmp.Params // community, versión; Target se ignora
	LocalIP      string      // IP de la interfaz a la que se enlaza el socket (opcional)
	Timeout      time.Duration
	Concurrencia int
	Dialer       snmp.Dialer // nil = snmp.Dial
	// Candidata decide si a una respuesta vale la pena pedirle el modelo.
	// nil = a ninguna.
	Candidata func(sysDescr string) bool
	// Progreso se llama tras cada host (desde varias goroutines, en serie).
	Progreso func(hechos, total int)
}

// Barrer manda GET sysDescr.0 a todos los hosts con concurrencia limitada y
// devuelve los que contestaron, ordenados por IP. Cancela con ctx.
func Barrer(ctx context.Context, hosts []net.IP, o OpcionesBarrido) ([]Respuesta, error) {
	if o.Concurrencia <= 0 {
		o.Concurrencia = 64
	}
	if o.Timeout <= 0 {
		o.Timeout = 800 * time.Millisecond
	}
	dial := o.Dialer
	if dial == nil {
		dial = snmp.Dial
	}

	trabajos := make(chan net.IP)
	var (
		mu     sync.Mutex
		out    []Respuesta
		hechos int
		wg     sync.WaitGroup
	)
	trabajador := func() {
		defer wg.Done()
		for ip := range trabajos {
			r, ok := sondear(ctx, ip.String(), o, dial)
			mu.Lock()
			if ok {
				out = append(out, r)
			}
			hechos++
			if o.Progreso != nil {
				o.Progreso(hechos, len(hosts))
			}
			mu.Unlock()
		}
	}
	nw := o.Concurrencia
	if nw > len(hosts) {
		nw = len(hosts)
	}
	wg.Add(nw)
	for i := 0; i < nw; i++ {
		go trabajador()
	}
enviar:
	for _, ip := range hosts {
		select {
		case trabajos <- ip:
		case <-ctx.Done():
			break enviar
		}
	}
	close(trabajos)
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		return binary.BigEndian.Uint32(net.ParseIP(out[i].IP).To4()) < binary.BigEndian.Uint32(net.ParseIP(out[j].IP).To4())
	})
	return out, ctx.Err()
}

func sondear(ctx context.Context, ip string, o OpcionesBarrido, dial snmp.Dialer) (Respuesta, bool) {
	if ctx.Err() != nil {
		return Respuesta{}, false
	}
	p := o.Base
	p.Target = ip
	p.Timeout = o.Timeout
	p.Retries = 0
	p.LocalIP = o.LocalIP
	c, err := dial(p)
	if err != nil {
		return Respuesta{}, false
	}
	defer c.Close()
	sd, err := snmp.SysDescr(ctx, c)
	if err != nil {
		return Respuesta{}, false
	}
	r := Respuesta{IP: ip, SysDescr: sd}
	if o.Candidata != nil && o.Candidata(sd) {
		r.Modelo = snmp.DeviceModel(ctx, c)
	}
	return r, true
}
