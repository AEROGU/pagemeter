package brother

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
)

type esperado struct {
	total                                 int64
	pc, copia, fax                        [3]*int64 // total, color, mono
	funcionesRaw, tamanosRaw              string
	nFunciones, nTamanos, nDesconocidasFn int
}

func p(n int64) *int64 { return &n }

// Impresora A: color, sin fax. Impresora B: DCP-L5650DN, monocromática.
var fixtureEsperado = []esperado{
	{
		total: 2399,
		pc:    [3]*int64{p(2050), p(2038), p(12)},
		copia: [3]*int64{p(207), p(58), p(149)},
		fax:   [3]*int64{nil, nil, nil},
		funcionesRaw: "1=2050;5=2038;7=12;3=207;4=58;9=149;11=50;15=49;14=1;12=0;17=0;16=0;18=0;20=0;19=0;" +
			"91=1;93=1;92=0;94=2;96=2;95=0",
		tamanosRaw: "702=161;703=2238;898=2399;928=0;999=0",
		nFunciones: 21, nTamanos: 5, nDesconocidasFn: 15,
	},
	{
		total:        128951,
		pc:           [3]*int64{p(64002), nil, nil},
		copia:        [3]*int64{p(64850), nil, nil},
		fax:          [3]*int64{p(0), nil, nil},
		funcionesRaw: "1=64002;3=64850;2=0;91=101;11=12938;12=10329;13=0;94=0",
		tamanosRaw:   "898=98197;909=30693;899=2;900=9;25=10;999=42",
		nFunciones:   8, nTamanos: 6, nDesconocidasFn: 5,
	},
}

func TestParseTableFixture(t *testing.T) {
	walks := cargarFixture(t)
	fun, err := ParseTable(walks[1], TablaFunciones)
	if err != nil {
		t.Fatal(err)
	}
	want := []Fila{{1, 1, 64002}, {2, 3, 64850}, {3, 2, 0}, {4, 91, 101}, {5, 11, 12938}, {6, 12, 10329}, {7, 13, 0}, {8, 94, 0}}
	if len(fun) != len(want) {
		t.Fatalf("filas = %d, se esperaban %d: %+v", len(fun), len(want), fun)
	}
	for i := range want {
		if fun[i] != want[i] {
			t.Errorf("fila %d = %+v, se esperaba %+v", i, fun[i], want[i])
		}
	}
	// Una tabla que no existe da lista vacía, sin error.
	if f, err := ParseTable(walks[1], 77); err != nil || len(f) != 0 {
		t.Errorf("tabla inexistente: %v, %v", f, err)
	}
}

func TestParseTableOmiteFilasIncompletas(t *testing.T) {
	r := OIDPrintPages + ".21.1."
	walk := []snmp.Var{
		snmp.IntVar(r+"2.1", 1), snmp.IntVar(r+"3.1", 10),
		snmp.IntVar(r+"2.2", 3),                            // sin columna 3
		snmp.IntVar(r+"3.3", 5),                            // sin columna 2
		snmp.StrVar(r+"2.4", "x"), snmp.IntVar(r+"3.4", 7), // código no numérico
	}
	fs, err := ParseTable(walk, 21)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0] != (Fila{1, 1, 10}) {
		t.Errorf("resultado = %+v", fs)
	}
}

func TestDecodificarFixture(t *testing.T) {
	walks := cargarFixture(t)
	for i, w := range walks {
		e := fixtureEsperado[i]
		var r counters.Reading
		if err := Decodificar(soloRama(w, OIDPrintPages), &r); err != nil {
			t.Fatalf("impresora %d: %v", i, err)
		}
		comprobar(t, i, r, e, false)
	}
}

func TestLeerConMockFixture(t *testing.T) {
	walks := cargarFixture(t)
	reg := counters.NuevoRegistro(Driver{})
	for i, w := range walks {
		vars := append([]snmp.Var{snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8300h, Firmware Ver.1.00")}, w...)
		r, err := reg.Leer(context.Background(), snmp.NewMock(vars...), counters.MarcaAuto)
		if err != nil {
			t.Fatalf("impresora %d: %v", i, err)
		}
		if r.Marca != "brother" {
			t.Errorf("impresora %d: marca = %q", i, r.Marca)
		}
		if len(r.Avisos) != 0 {
			t.Errorf("impresora %d: avisos inesperados %v", i, r.Avisos)
		}
		// Crudos: sysDescr + total + toda la rama .52 del fixture.
		if len(r.Crudos) != len(w)+1 {
			t.Errorf("impresora %d: crudos = %d, se esperaban %d", i, len(r.Crudos), len(w)+1)
		}
		comprobar(t, i, r, fixtureEsperado[i], true)
	}
}

func TestLeerFallaTablaBrotherConservaTotal(t *testing.T) {
	walks := cargarFixture(t)
	m := snmp.NewMock(append([]snmp.Var{snmp.StrVar(snmp.OIDSysDescr, "Brother X")}, walks[0]...)...)
	m.FailWalk = map[string]error{OIDPrintPages: errors.New("timeout simulado")}
	r, err := counters.NuevoRegistro(Driver{}).Leer(context.Background(), m, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if r.Total == nil || *r.Total != 2399 {
		t.Errorf("total = %v", r.Total)
	}
	if r.PC.Total != nil || len(r.Funciones) != 0 {
		t.Errorf("no debió haber desglose: %+v", r.PC)
	}
	if len(r.Avisos) != 1 || !strings.Contains(r.Avisos[0], "timeout simulado") {
		t.Errorf("avisos = %v", r.Avisos)
	}
}

func TestLeerSinRespuesta(t *testing.T) {
	m := &snmp.Mock{Err: snmp.ErrNoResponse}
	_, err := counters.NuevoRegistro(Driver{}).Leer(context.Background(), m, "auto")
	if !errors.Is(err, snmp.ErrNoResponse) {
		t.Fatalf("err = %v", err)
	}
}

func TestElegirDriver(t *testing.T) {
	reg := counters.NuevoRegistro(Driver{})
	cases := []struct{ marca, sd, want string }{
		{"auto", "Brother NC-8300h", "brother"},
		{"", "BROTHER HL-L2350DW", "brother"},
		{"auto", "HP ETHERNET MULTI-ENVIRONMENT", "generico"},
		{"brother", "cualquier cosa", "brother"},
		{"Generico", "Brother", "generico"},
	}
	for _, c := range cases {
		d, err := reg.Elegir(c.marca, c.sd)
		if err != nil || d.Marca() != c.want {
			t.Errorf("Elegir(%q,%q) = %v, %v; se esperaba %s", c.marca, c.sd, d, err, c.want)
		}
	}
	if _, err := reg.Elegir("epson", "x"); err == nil {
		t.Error("marca sin driver debió dar error")
	}
	if !reg.Reconoce("Brother X") || reg.Reconoce("HP X") {
		t.Error("Reconoce no distingue marcas")
	}
}

func TestNombres(t *testing.T) {
	if n, ok := NombreFuncion(5); !ok || n != "PC: color" {
		t.Errorf("función 5 = %q, %v", n, ok)
	}
	if n, ok := NombreFuncion(91); ok || n != "Código 91 (no documentado)" {
		t.Errorf("función 91 = %q, %v", n, ok)
	}
	if n, ok := NombreTamano(898); !ok || n != "A4 / Carta" {
		t.Errorf("tamaño 898 = %q, %v", n, ok)
	}
}

func comprobar(t *testing.T, i int, r counters.Reading, e esperado, conTotal bool) {
	t.Helper()
	if conTotal && (r.Total == nil || *r.Total != e.total) {
		t.Errorf("impresora %d: total = %v, se esperaba %d", i, fmtP(r.Total), e.total)
	}
	cmp := func(nombre string, d counters.Desglose, w [3]*int64) {
		got := [3]*int64{d.Total, d.Color, d.Mono}
		for k, lbl := range []string{"total", "color", "mono"} {
			if !eqP(got[k], w[k]) {
				t.Errorf("impresora %d: %s %s = %s, se esperaba %s", i, nombre, lbl, fmtP(got[k]), fmtP(w[k]))
			}
		}
	}
	cmp("pc", r.PC, e.pc)
	cmp("copia", r.Copia, e.copia)
	cmp("fax", r.Fax, e.fax)
	if got := counters.FormatRaw(r.Funciones); got != e.funcionesRaw {
		t.Errorf("impresora %d: funciones_raw =\n %s\nse esperaba\n %s", i, got, e.funcionesRaw)
	}
	if got := counters.FormatRaw(r.Tamanos); got != e.tamanosRaw {
		t.Errorf("impresora %d: tamanos_raw = %s, se esperaba %s", i, got, e.tamanosRaw)
	}
	if len(r.Funciones) != e.nFunciones || len(r.Tamanos) != e.nTamanos {
		t.Errorf("impresora %d: %d funciones / %d tamaños", i, len(r.Funciones), len(r.Tamanos))
	}
	desc := 0
	for _, l := range r.Funciones {
		if !l.Conocido {
			desc++
			if !strings.Contains(l.Nombre, "no documentado") {
				t.Errorf("impresora %d: código desconocido sin marcar: %+v", i, l)
			}
		}
	}
	if desc != e.nDesconocidasFn {
		t.Errorf("impresora %d: %d funciones desconocidas, se esperaban %d", i, desc, e.nDesconocidasFn)
	}
}

func eqP(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func fmtP(a *int64) string {
	if a == nil {
		return "<vacío>"
	}
	return strconv.FormatInt(*a, 10)
}
