package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/store"
	"github.com/AEROGU/pagemeter/internal/vendors/brother"
)

// TestTUIReal: escaneo desde la TUI contra impresoras reales (SNMP real) y el
// barrido de red. Sólo con PAGEMETER_TUI_REAL=<ip1,ip2,...>.
func TestTUIReal(t *testing.T) {
	if os.Getenv("PAGEMETER_TUI_REAL") == "" {
		t.Skip("define PAGEMETER_TUI_REAL con las IPs separadas por comas")
	}
	ips := strings.Split(os.Getenv("PAGEMETER_TUI_REAL"), ",")
	cfg := config.Default()
	cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	for _, ip := range ips {
		_ = cfg.Agregar(config.Printer{IP: ip, Origen: config.OrigenManual, Habilitada: true})
	}
	_ = cfg.Save()
	scr := tcell.NewSimulationScreen("UTF-8")
	ui := New(cfg, counters.NuevoRegistro(brother.Driver{}), nil)
	ui.App().SetScreen(scr)
	p := &prueba{t: t, ui: ui, scr: scr, fin: make(chan error, 1)}
	go func() { p.fin <- ui.Run() }()
	t.Cleanup(func() { ui.Detener(); <-p.fin })
	scr.SetSize(170, 45)

	p.esperar("habilitadas)")
	p.tecla('s')
	esperarFilasLargo(t, cfg.CSVAbs(), len(ips))
	s := p.esperar("Total: ", "Escaneo ")
	t.Logf("\n%s", s)
	fs, _, _ := store.Leer(cfg.CSVAbs())
	for _, f := range fs {
		t.Logf("%s %s total=%s err=%s", f.IP, f.Estado, fmtNum(f.Num[store.ColTotal]), f.Error)
	}

	p.tecla('1')
	p.tecla('r')
	p.esperar("Elige la red", "Wi-Fi")
	p.especial(tcell.KeyDown)
	p.especial(tcell.KeyDown)
	p.especial(tcell.KeyEnter)
	for i := 0; i < 300 && !strings.Contains(p.pantalla(), "Listo en"); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("\n%s", p.pantalla())
}

func esperarFilasLargo(t *testing.T, csv string, n int) {
	for i := 0; i < 600; i++ {
		if fs, _, _ := store.Leer(csv); len(fs) >= n {
			return
		}
		<-timeAfter()
	}
	t.Fatal("el escaneo real no terminó")
}

func timeAfter() <-chan struct{} {
	ch := make(chan struct{})
	go func() { tiempoDormir(); close(ch) }()
	return ch
}

func tiempoDormir() { time.Sleep(100 * time.Millisecond) }
