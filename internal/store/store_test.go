package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AEROGU/pagemeter/internal/counters"
)

func p(n int64) *int64 { return &n }

var t0 = time.Date(2026, 11, 17, 14, 5, 12, 0, time.Local)

// lecturaB reproduce la impresora B del fixture (DCP-L5650DN, monocromática).
func lecturaB() counters.Reading {
	return counters.Reading{
		Marca:    "brother",
		SysDescr: "Brother NC-8900h, Firmware Ver.ZT  ,MID 8C5-J25,FID 2",
		Total:    p(128951),
		PC:       counters.Desglose{Total: p(64002)},
		Copia:    counters.Desglose{Total: p(64850)},
		Fax:      counters.Desglose{Total: p(0)},
		Funciones: []counters.Linea{
			{Codigo: 1, Paginas: 64002}, {Codigo: 3, Paginas: 64850}, {Codigo: 91, Paginas: 101},
		},
		Tamanos: []counters.Linea{{Codigo: 898, Paginas: 98197}, {Codigo: 909, Paginas: 30693}},
	}
}

func filasEscaneo(fecha time.Time) []Fila {
	id := NuevoEscaneoID(fecha)
	return []Fila{
		DesdeLectura(fecha, id, "Brother DCP-L5650DN series", "192.168.1.104", lecturaB(), nil),
		DesdeLectura(fecha, id, "Recepción, planta baja", "192.168.1.50", counters.Reading{}, errors.New("192.168.1.50: sin respuesta SNMP")),
	}
}

func TestAppendNuevoYAcumulado(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contadores.csv")
	if err := Append(path, filasEscaneo(t0)); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, filasEscaneo(t0.Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, bom) {
		t.Fatal("falta el BOM")
	}
	if bytes.Count(data, bom) != 1 {
		t.Error("el BOM aparece más de una vez")
	}
	txt := string(data[3:])
	enc := strings.Join(Encabezado, ",")
	if strings.Count(txt, enc) != 1 || !strings.HasPrefix(txt, enc+"\r\n") {
		t.Errorf("el encabezado debe aparecer una sola vez, al inicio")
	}
	lineas := strings.Split(strings.TrimRight(txt, "\r\n"), "\r\n")
	if len(lineas) != 5 {
		t.Fatalf("líneas = %d, se esperaban 5:\n%s", len(lineas), txt)
	}
	// Fila ok: celdas vacías donde la impresora no reporta (pc_color, pc_mono...), no 0.
	wantOK := "2026-11-17 14:05:12,20261117-140512,Brother DCP-L5650DN series,brother,192.168.1.104," +
		"\"Brother NC-8900h, Firmware Ver.ZT ,MID 8C5-J25,FID 2\",ok,," +
		"128951,64002,,,64850,,,0,,," +
		"1=64002;3=64850;91=101,898=98197;909=30693"
	if lineas[1] != wantOK {
		t.Errorf("fila ok =\n%s\nse esperaba\n%s", lineas[1], wantOK)
	}
	// Fila con error: se escribe igual, con todas las numéricas vacías.
	wantErr := "2026-11-17 14:05:12,20261117-140512,\"Recepción, planta baja\",,192.168.1.50,,error," +
		"192.168.1.50: sin respuesta SNMP,,,,,,,,,,,,"
	if lineas[2] != wantErr {
		t.Errorf("fila error =\n%s\nse esperaba\n%s", lineas[2], wantErr)
	}
}

func TestLeerIdaYVuelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.csv")
	orig := filasEscaneo(t0)
	orig[0].SysDescr = "línea 1\r\nlínea 2" // se aplana al escribir
	if err := Append(path, orig); err != nil {
		t.Fatal(err)
	}
	fs, avisos, err := Leer(path)
	if err != nil || len(avisos) != 0 {
		t.Fatalf("Leer: %v %v", err, avisos)
	}
	if len(fs) != 2 {
		t.Fatalf("filas = %d", len(fs))
	}
	a := fs[0]
	if !a.Fecha.Equal(t0) || a.EscaneoID != "20261117-140512" || a.Marca != "brother" || a.SysDescr != "línea 1 línea 2" {
		t.Errorf("fila = %+v", a)
	}
	if *a.Valor("total") != 128951 || a.Valor("pc_color") != nil || *a.Valor("fax_total") != 0 {
		t.Error("valores numéricos no conservados (vacío ≠ 0)")
	}
	if fs[1].Impresora != "Recepción, planta baja" || fs[1].Estado != EstadoError || fs[1].Num[ColTotal] != nil {
		t.Errorf("fila error = %+v", fs[1])
	}
}

func TestLeerNoExiste(t *testing.T) {
	fs, av, err := Leer(filepath.Join(t.TempDir(), "nada.csv"))
	if err != nil || fs != nil || av != nil {
		t.Errorf("%v %v %v", fs, av, err)
	}
}

func TestAppendRechazaOtroEncabezado(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.csv")
	viejo := string(bom) + "Fecha,Impresora,IP,Total\r\n2026-01-01 10:00,X,1.2.3.4,5\r\n"
	if err := os.WriteFile(path, []byte(viejo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, filasEscaneo(t0)); err == nil {
		t.Fatal("debió rechazar un CSV con otro encabezado")
	}
	if got, _ := os.ReadFile(path); string(got) != viejo {
		t.Error("modificó el archivo a pesar del error")
	}
}

func TestAppendSinSaltoFinal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.csv")
	if err := Append(path, filasEscaneo(t0)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.TrimRight(data, "\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, filasEscaneo(t0.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	fs, av, err := Leer(path)
	if err != nil || len(av) != 0 || len(fs) != 4 {
		t.Errorf("filas=%d avisos=%v err=%v", len(fs), av, err)
	}
}

func TestParseToleranteYAvisos(t *testing.T) {
	csv := "ip,fecha,estado,impresora,escaneo_id,total,extra\n" +
		"10.0.0.1,2026-11-01 08:00:00,ok,A,s1,100,x\n" +
		"10.0.0.1,fecha mala,ok,A,s1,100,x\n" +
		"10.0.0.1,2026-11-01 08:00:00,ok,A,s1,cien,x\n" +
		"\n"
	fs, av, err := Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || *fs[0].Num[ColTotal] != 100 || fs[0].Num[ColPCTotal] != nil {
		t.Errorf("filas = %+v", fs)
	}
	if len(av) != 2 || !strings.Contains(av[0], "línea 3") || !strings.Contains(av[1], "línea 4") {
		t.Errorf("avisos = %v", av)
	}
	if _, _, err := Parse(strings.NewReader("a,b\n1,2\n")); err == nil {
		t.Error("sin columnas obligatorias debió fallar")
	}
}

func TestAgruparYFiltrar(t *testing.T) {
	var fs []Fila
	fs = append(fs, filasEscaneo(t0)...)
	fs = append(fs, filasEscaneo(t0.Add(48*time.Hour))...)
	fs = append(fs, filasEscaneo(t0.Add(24*time.Hour))...)
	es := Agrupar(fs)
	if len(es) != 3 || es[0].ID != "20261119-140512" || es[2].ID != "20261117-140512" {
		t.Fatalf("orden = %v", []string{es[0].ID, es[1].ID, es[2].ID})
	}
	if es[0].OK() != 1 || es[0].Errores() != 1 {
		t.Error("conteo de estados")
	}
	f := FiltrarImpresora(es, "RECEPCIÓN")
	if len(f) != 3 || len(f[0].Filas) != 1 || f[0].Filas[0].IP != "192.168.1.50" {
		t.Errorf("filtro por nombre = %+v", f)
	}
	if f := FiltrarImpresora(es, "1.104"); len(f) != 3 || f[0].Filas[0].IP != "192.168.1.104" {
		t.Error("filtro por IP")
	}
	if f := FiltrarImpresora(es, "no existe"); len(f) != 0 {
		t.Error("filtro sin coincidencias")
	}
	u := UltimaPorImpresora(fs)
	if !u["192.168.1.104"].Fecha.Equal(t0.Add(48 * time.Hour)) {
		t.Error("UltimaPorImpresora")
	}
}

func TestDiferencia(t *testing.T) {
	a := filasEscaneo(t0)
	b := filasEscaneo(t0.Add(30 * 24 * time.Hour))
	b[0].Num[ColTotal] = p(130000)
	b[0].Num[ColPCTotal] = p(65000)
	b[0].Num[ColCopiaTotal] = p(64901)
	b[1].Estado = EstadoOK // la de recepción respondió en el segundo
	nueva := DesdeLectura(b[0].Fecha, b[0].EscaneoID, "Nueva", "192.168.1.200", lecturaB(), nil)
	b = append(b, nueva)
	es := Agrupar(append(append([]Fila{}, a...), b...))

	// Pasados en orden inverso (reciente primero), como los da Agrupar.
	antes, despues, d := Diferencia(es[0], es[1])
	if antes.ID != a[0].EscaneoID || despues.ID != b[0].EscaneoID {
		t.Fatalf("no ordenó por fecha: %s → %s", antes.ID, despues.ID)
	}
	if len(d) != 3 {
		t.Fatalf("filas = %d", len(d))
	}
	m := map[string]DifFila{}
	for _, x := range d {
		m[x.IP] = x
	}
	x := m["192.168.1.104"]
	if *x.Delta[ColTotal] != 1049 || *x.Delta[ColPCTotal] != 998 || *x.Delta[ColCopiaTotal] != 51 ||
		*x.Delta[ColFaxTotal] != 0 || x.Delta[ColPCColor] != nil || x.Nota != "" {
		t.Errorf("delta 104 = %+v", x)
	}
	if m["192.168.1.50"].Nota != "error en el escaneo inicial" || m["192.168.1.50"].Delta[ColTotal] != nil {
		t.Errorf("recepción = %+v", m["192.168.1.50"])
	}
	if m["192.168.1.200"].Nota != "sólo en el escaneo final" {
		t.Errorf("nueva = %+v", m["192.168.1.200"])
	}

	// Contador que baja.
	c := filasEscaneo(t0.Add(60 * 24 * time.Hour))
	c[0].Num[ColTotal] = p(10)
	_, _, d2 := Diferencia(Escaneo{Fecha: t0, Filas: a}, Escaneo{Fecha: c[0].Fecha, Filas: c[:1]})
	if *d2[0].Delta[ColTotal] != 10-128951 || !strings.Contains(d2[0].Nota, "bajó") {
		t.Errorf("contador que baja = %+v", d2[0])
	}
	if len(d2) != 2 || d2[1].Nota != "sólo en el escaneo inicial" {
		t.Errorf("impresora que desaparece = %+v", d2)
	}
}

func TestAvisosEnColumnaError(t *testing.T) {
	r := lecturaB()
	r.Avisos = []string{"brother: timeout"}
	f := DesdeLectura(t0, "x", "A", "1.1.1.1", r, nil)
	if f.Estado != EstadoOK || f.Error != "brother: timeout" {
		t.Errorf("fila = %+v", f)
	}
}
