package discovery

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

func lookupFalso(ctx context.Context, h string) ([]string, error) {
	if strings.EqualFold(h, "BRWAABBCCDDEE01") {
		return []string{"fe80::1", "10.0.0.178"}, nil
	}
	return nil, errors.New("no such host")
}

func cargar(t *testing.T) Crudo {
	t.Helper()
	b, err := os.ReadFile("testdata/instaladas.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCrudo(b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseCrudoFixture(t *testing.T) {
	c := cargar(t)
	if len(c.Printers) != 13 || len(c.Devices) != 10 || len(c.Neighbors) != 3 {
		t.Fatalf("colas=%d dispositivos=%d vecinos=%d", len(c.Printers), len(c.Devices), len(c.Neighbors))
	}
	// "Props" y "Values" sueltos (un solo elemento) también se aceptan.
	if len(c.Devices[2].Props) != 1 || len(c.Devices[1].Props[1].Values) != 1 {
		t.Error("objeto suelto no convertido a lista")
	}
	if c.Devices[9].Props != nil {
		t.Error("dispositivo sin propiedades")
	}
}

func TestParseCrudoFormasRaras(t *testing.T) {
	// Una sola impresora: ConvertTo-Json la emite como objeto, no como arreglo.
	c, err := ParseCrudo([]byte(`{"Printers":{"Name":"Brother X","PortName":"10.1.1.1","HostAddress":"10.1.1.1"},"Devices":null}`))
	if err != nil || len(c.Printers) != 1 || c.Printers[0].Name != "Brother X" || c.Devices != nil {
		t.Fatalf("%+v %v", c, err)
	}
	// Valores numéricos o booleanos en propiedades PnP.
	c, err = ParseCrudo([]byte(`{"Devices":[{"Id":"A","Props":[{"Key":"k","Values":[3,true,null,"x"]}]}]}`))
	if err != nil || len(c.Devices[0].Props[0].Values) != 4 || c.Devices[0].Props[0].Values[0] != "3" {
		t.Fatalf("%+v %v", c, err)
	}
	// Salida vacía (no hay nada que listar).
	if c, err := ParseCrudo([]byte("  \r\n")); err != nil || len(c.Printers) != 0 {
		t.Error("salida vacía")
	}
	if _, err := ParseCrudo([]byte("Get-Printer : error")); err == nil {
		t.Error("texto no JSON debió fallar")
	}
}

func TestResolverFixture(t *testing.T) {
	got := Resolver(context.Background(), cargar(t), "Brother", lookupFalso)
	want := map[string]struct{ ip, metodo string }{
		"Brother HL-1110":                           {"", ""},
		"Brother HL-5100DN":                         {"10.0.0.77", "nombre del puerto"},
		"Brother HL-L2350DW":                        {"192.168.0.50", "puerto TCP/IP"},
		"Brother DCP-T730DW Printer":                {"10.0.0.178", "hostname BRWAABBCCDDEE01"},
		"Brother HL-L8360CDW series":                {"10.0.0.99", "WSD (por nombre de modelo, sin verificar)"},
		"Brother MFC-J6945DW Printer":               {"10.0.0.88", `WSD (dispositivo padre SWD\DAFWSDPROVIDER)`},
		"Brother MFC-L3780CDW series":               {"10.0.0.67", `WSD (dispositivo padre SWD\IPP)`},
		"Brother DCP-1610W [a1b2c3d4e5f6]":          {"10.0.0.31", "MAC del nombre en caché ARP"},
		"Brother DCP-L5650DN series [b4220000aa01]": {"10.0.0.104", `WSD (dispositivo padre SWD\IPP)`},
	}
	if len(got) != len(want) {
		for _, g := range got {
			t.Logf("%+v", g)
		}
		t.Fatalf("resultado = %d impresoras, se esperaban %d", len(got), len(want))
	}
	for _, g := range got {
		w, ok := want[g.Nombre]
		if !ok {
			t.Errorf("no se esperaba %q (¿FAX, filtro o duplicado?)", g.Nombre)
			continue
		}
		if g.IP != w.ip || g.Metodo != w.metodo {
			t.Errorf("%s: ip=%q método=%q, se esperaba %q %q", g.Nombre, g.IP, g.Metodo, w.ip, w.metodo)
		}
	}
	// La USB queda sin IP con un motivo legible.
	for _, g := range got {
		if g.Nombre == "Brother HL-1110" && !strings.Contains(g.Error, "USB001 no es de red") {
			t.Errorf("motivo = %q", g.Error)
		}
	}
}

func TestResolverColaBRNSinWSD(t *testing.T) {
	// Sólo la cola "(Copiar 1)": el hostname no resuelve y su MAC no está en ARP,
	// pero un dispositivo PnP con esa MAC en el id trae la IP.
	c := cargar(t)
	var ps Lista[ImpresoraWin]
	for _, p := range c.Printers {
		if strings.Contains(p.Name, "Copiar 1") {
			ps = append(ps, p)
		}
	}
	c.Printers = ps
	got := Resolver(context.Background(), c, "brother", lookupFalso)
	if len(got) != 1 || got[0].IP != "10.0.0.67" || got[0].Metodo != "MAC del hostname BRN0011223344AA en dispositivos PnP" {
		t.Fatalf("%+v", got)
	}
	// Sin el dispositivo PnP, queda sin IP y explica por qué.
	c.Devices = nil
	got = Resolver(context.Background(), c, "brother", lookupFalso)
	if got[0].IP != "" || !strings.Contains(got[0].Error, "BRN0011223344AA no resuelve") ||
		!strings.Contains(got[0].Error, "caché ARP") {
		t.Fatalf("%+v", got)
	}
}

func TestResolverSinFiltro(t *testing.T) {
	got := Resolver(context.Background(), cargar(t), "", lookupFalso)
	hayEpson, hayFax := false, false
	for _, g := range got {
		if strings.HasPrefix(g.Nombre, "EPSON") && g.IP == "10.0.0.170" {
			hayEpson = true
		}
		if strings.Contains(strings.ToUpper(g.Nombre), "FAX") {
			hayFax = true
		}
	}
	if !hayEpson || hayFax {
		t.Errorf("epson=%v fax=%v", hayEpson, hayFax)
	}
}

func TestModelo(t *testing.T) {
	cases := map[string]string{
		"Brother DCP-L5650DN series [b4220000aa01]": "DCP-L5650DN",
		"Brother MFC-L3780CDW series (Copiar 1)":    "MFC-L3780CDW",
		"Brother DCP-T730DW Printer":                "DCP-T730DW",
		"Brother HL-1110":                           "HL-1110",
	}
	for in, want := range cases {
		if got := Modelo(in); got != want {
			t.Errorf("Modelo(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}

func TestEncodeCommand(t *testing.T) {
	// "ñ" en UTF-16LE = F1 00; "a" = 61 00.
	if got := encodeCommand("añ"); got != "YQDxAA==" {
		t.Errorf("encodeCommand = %s", got)
	}
}

func TestParseCIDRYHosts(t *testing.T) {
	cases := []struct {
		in     string
		n      int
		first  string
		last   string
		valido bool
	}{
		{"192.168.1.0/24", 254, "192.168.1.1", "192.168.1.254", true},
		{"192.168.1.57/24", 254, "192.168.1.1", "192.168.1.254", true},
		{"10.0.16.0/20", 4094, "10.0.16.1", "10.0.31.254", true},
		{"10.0.0.8/30", 2, "10.0.0.9", "10.0.0.10", true},
		{"172.16.5.0/29", 6, "172.16.5.1", "172.16.5.6", true},
		{"10.0.0.0/19", 0, "", "", false},
		{"10.0.0.0/31", 0, "", "", false},
		{"10.0.0.0", 0, "", "", false},
		{"fe80::/64", 0, "", "", false},
		{"300.1.1.0/24", 0, "", "", false},
	}
	for _, c := range cases {
		red, err := ParseCIDR(c.in)
		if !c.valido {
			if err == nil {
				t.Errorf("%s: se esperaba error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		hs, err := Hosts(red)
		if err != nil || len(hs) != c.n || hs[0].String() != c.first || hs[len(hs)-1].String() != c.last {
			t.Errorf("%s: %d hosts [%v … %v] %v", c.in, len(hs), hs[0], hs[len(hs)-1], err)
		}
	}
}

func TestBarrer(t *testing.T) {
	red, _ := ParseCIDR("10.9.9.0/28")
	hosts, _ := Hosts(red)
	mocks := map[string]*snmp.Mock{
		"10.9.9.5": snmp.NewMock(snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h"),
			snmp.StrVar(snmp.OIDHrDeviceDescr+".1", "Brother DCP-L5650DN series")),
		"10.9.9.2":  snmp.NewMock(snmp.StrVar(snmp.OIDSysDescr, "Linux router"), snmp.StrVar(snmp.OIDHrDeviceDescr+".1", "no se pide")),
		"10.9.9.14": snmp.NewMock(snmp.StrVar(snmp.OIDSysDescr, "BROTHER NC-510w")),
	}
	var ultimo, maximo atomic.Int64
	var locales []string
	dial := snmp.MockDialer(mocks)
	res, err := Barrer(context.Background(), hosts, OpcionesBarrido{
		Concurrencia: 4,
		LocalIP:      "10.9.9.100",
		Dialer: func(p snmp.Params) (snmp.Client, error) {
			if p.Retries != 0 || p.Timeout != 800*time.Millisecond {
				t.Errorf("parámetros = %+v", p)
			}
			if p.LocalIP != "10.9.9.100" {
				locales = append(locales, p.LocalIP)
			}
			return dial(p)
		},
		Candidata: func(sd string) bool { return strings.Contains(strings.ToLower(sd), "brother") },
		Progreso: func(h, total int) {
			if total != 14 || int64(h) <= ultimo.Load() {
				t.Errorf("progreso %d/%d no crece", h, total)
			}
			ultimo.Store(int64(h))
			if int64(h) > maximo.Load() {
				maximo.Store(int64(h))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if maximo.Load() != 14 {
		t.Errorf("progreso final = %d", maximo.Load())
	}
	if len(locales) > 0 {
		t.Errorf("no se enlazó a la IP local: %v", locales)
	}
	if len(res) != 3 || res[0].IP != "10.9.9.2" || res[1].IP != "10.9.9.5" || res[2].IP != "10.9.9.14" {
		t.Fatalf("respuestas = %+v", res)
	}
	if res[1].Modelo != "Brother DCP-L5650DN series" || res[0].Modelo != "" || res[2].Modelo != "" {
		t.Errorf("modelos = %q %q %q", res[0].Modelo, res[1].Modelo, res[2].Modelo)
	}
}

func TestBarrerCancelado(t *testing.T) {
	red, _ := ParseCIDR("10.9.0.0/20")
	hosts, _ := Hosts(red)
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int64
	_, err := Barrer(ctx, hosts, OpcionesBarrido{
		Concurrencia: 2,
		Dialer:       snmp.MockDialer(nil),
		Progreso: func(h, _ int) {
			if n.Add(1) == 10 {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if n.Load() >= int64(len(hosts)) {
		t.Error("no se detuvo al cancelar")
	}
}

func TestInterfacesSinLoopback(t *testing.T) {
	ifs, err := Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range ifs {
		if i.IP.IsLoopback() || i.IP.IsLinkLocalUnicast() || i.IP.To4() == nil || !i.Red.Contains(i.IP) {
			t.Errorf("interfaz inválida: %+v", i)
		}
		t.Logf("%s  %s  %s", i.Nombre, i.IP, i.CIDR())
	}
	_ = net.IPv4len
}

func TestVerificar(t *testing.T) {
	ins := []Instalada{
		{Nombre: "A", IP: "10.0.0.1"},
		{Nombre: "B", IP: "10.0.0.2"}, // IP vieja: no responde
		{Nombre: "C"},                 // sin IP: no se toca
	}
	dial := snmp.MockDialer(map[string]*snmp.Mock{"10.0.0.1": snmp.NewMock(snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h"))})
	Verificar(context.Background(), ins, snmp.Params{}, dial)
	if !ins[0].Verificada || !ins[0].Responde || ins[0].SysDescr != "Brother NC-8900h" {
		t.Errorf("A = %+v", ins[0])
	}
	if !ins[1].Verificada || ins[1].Responde {
		t.Errorf("B = %+v", ins[1])
	}
	if ins[2].Verificada {
		t.Errorf("C = %+v", ins[2])
	}
}
