package escaneo

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/store"
	"github.com/AEROGU/pagemeter/internal/vendors/brother"
)

const t21 = brother.OIDPrintPages + ".21.1."

func mockBrother(total, pc int64) *snmp.Mock {
	return snmp.NewMock(
		snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h"),
		snmp.IntVar(snmp.OIDPrtMarkerLifeCount+".1.1", total),
		snmp.IntVar(t21+"2.1", 1), snmp.IntVar(t21+"3.1", pc),
		snmp.StrVar(snmp.OIDHrDeviceDescr+".1", "Brother DCP-L5650DN series"),
	)
}

func cfgPrueba(t *testing.T) *config.Config {
	c := config.Default()
	c.SetPath(filepath.Join(t.TempDir(), "config.json"))
	for _, p := range []config.Printer{
		{Nombre: "A", IP: "10.0.0.1", Origen: config.OrigenManual, Habilitada: true},
		{Nombre: "B", IP: "10.0.0.2", Origen: config.OrigenManual, Habilitada: true},
		{Nombre: "Apagada", IP: "10.0.0.3", Origen: config.OrigenManual, Habilitada: false},
		{IP: "10.0.0.4", Origen: config.OrigenRed, Habilitada: true}, // no responde
	} {
		if err := c.Agregar(p); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestEjecutar(t *testing.T) {
	cfg := cfgPrueba(t)
	dial := snmp.MockDialer(map[string]*snmp.Mock{
		"10.0.0.1": mockBrother(1000, 600),
		"10.0.0.2": mockBrother(50, 40),
		"10.0.0.3": mockBrother(1, 1),
	})
	reg := counters.NuevoRegistro(brother.Driver{})
	var mu sync.Mutex
	ultimos := map[int]string{}
	res := Ejecutar(context.Background(), cfg, reg, dial, func(i int, e string, _ error) {
		mu.Lock()
		ultimos[i] = e
		mu.Unlock()
	})
	if res.CSVErr != nil {
		t.Fatal(res.CSVErr)
	}
	if len(res.Lecturas) != 3 || res.Fallidas() != 1 {
		t.Fatalf("lecturas=%d fallidas=%d", len(res.Lecturas), res.Fallidas())
	}
	if ultimos[0] != EstadoOK || ultimos[1] != EstadoOK || ultimos[2] != EstadoError {
		t.Errorf("progreso final = %v", ultimos)
	}
	if res.Lecturas[2].Fila.Impresora != "10.0.0.4" || res.Lecturas[2].Fila.Estado != store.EstadoError {
		t.Errorf("fila sin nombre = %+v", res.Lecturas[2].Fila)
	}
	fs, _, err := store.Leer(cfg.CSVAbs())
	if err != nil || len(fs) != 3 {
		t.Fatalf("CSV: %d filas, %v", len(fs), err)
	}
	for _, f := range fs {
		if f.EscaneoID != res.ID || !f.Fecha.Equal(res.Fecha) {
			t.Errorf("fila de otro escaneo: %+v", f)
		}
	}
	if *fs[0].Num[store.ColTotal] != 1000 || *fs[0].Num[store.ColPCTotal] != 600 || fs[0].Marca != "brother" {
		t.Errorf("fila A = %+v", fs[0])
	}
}

func TestEjecutarCancelado(t *testing.T) {
	cfg := cfgPrueba(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Ejecutar(ctx, cfg, counters.NuevoRegistro(brother.Driver{}), snmp.MockDialer(nil), nil)
	if res.CSVErr == nil {
		t.Fatal("cancelado no debe escribir el CSV")
	}
	if fs, _, _ := store.Leer(cfg.CSVAbs()); len(fs) != 0 {
		t.Error("se escribió el CSV")
	}
}

func TestProbar(t *testing.T) {
	cfg := cfgPrueba(t)
	dial := snmp.MockDialer(map[string]*snmp.Mock{"10.0.0.1": mockBrother(1000, 600)})
	reg := counters.NuevoRegistro(brother.Driver{})
	pr, err := Probar(context.Background(), cfg, reg, dial, "10.0.0.1", "auto")
	if err != nil || pr.Modelo != "Brother DCP-L5650DN series" || *pr.Reading.Total != 1000 {
		t.Fatalf("%+v %v", pr, err)
	}
	if _, err := Probar(context.Background(), cfg, reg, dial, "10.0.0.9", "auto"); err == nil {
		t.Error("IP sin respuesta debió fallar")
	}
}
