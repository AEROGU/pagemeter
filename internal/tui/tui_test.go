package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/discovery"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/store"
	"github.com/AEROGU/pagemeter/internal/vendors/brother"
)

type prueba struct {
	t   *testing.T
	ui  *UI
	scr tcell.SimulationScreen
	fin chan error
}

func arrancar(t *testing.T) (*prueba, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	_ = cfg.Agregar(config.Printer{Nombre: "Brother DCP-L5650DN series [b4220000aa01]", IP: "10.0.0.1", Origen: config.OrigenInstalada, Habilitada: true})
	_ = cfg.Agregar(config.Printer{IP: "10.0.0.2", Origen: config.OrigenManual, Habilitada: true})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t21 := brother.OIDPrintPages + ".21.1."
	t1 := brother.OIDPrintPages + ".1.1."
	dial := snmp.MockDialer(map[string]*snmp.Mock{
		"10.0.0.1": snmp.NewMock(
			snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h, Firmware Ver.ZT"),
			snmp.IntVar(snmp.OIDPrtMarkerLifeCount+".1.1", 128951),
			snmp.IntVar(t21+"2.1", 1), snmp.IntVar(t21+"3.1", 64002),
			snmp.IntVar(t21+"2.2", 91), snmp.IntVar(t21+"3.2", 101),
			snmp.IntVar(t1+"2.1", 898), snmp.IntVar(t1+"3.1", 98197),
			snmp.IntVar(t1+"2.2", 999), snmp.IntVar(t1+"3.2", 0),
		),
	})
	scr := tcell.NewSimulationScreen("UTF-8")
	ui := New(cfg, counters.NuevoRegistro(brother.Driver{}), dial)
	ui.App().SetScreen(scr)
	p := &prueba{t: t, ui: ui, scr: scr, fin: make(chan error, 1)}
	go func() { p.fin <- ui.Run() }()
	t.Cleanup(func() {
		ui.Detener()
		select {
		case <-p.fin:
		case <-time.After(5 * time.Second):
		}
	})
	scr.SetSize(170, 40)
	return p, cfg
}

// pantalla devuelve el texto en pantalla, leído desde la goroutine de tview.
func (p *prueba) pantalla() string {
	ch := make(chan string, 1)
	p.ui.App().QueueUpdate(func() {
		cells, w, _ := p.scr.GetContents()
		var b strings.Builder
		for i, c := range cells {
			if i > 0 && i%w == 0 {
				b.WriteByte('\n')
			}
			if len(c.Runes) > 0 {
				b.WriteString(string(c.Runes))
			} else {
				b.WriteByte(' ')
			}
		}
		ch <- b.String()
	})
	select {
	case s := <-ch:
		return s
	case <-time.After(3 * time.Second):
		p.t.Fatal("la UI no responde")
		return ""
	}
}

func (p *prueba) esperar(textos ...string) string {
	p.t.Helper()
	limite := time.Now().Add(5 * time.Second)
	var s string
	for time.Now().Before(limite) {
		s = p.pantalla()
		todos := true
		for _, x := range textos {
			if !strings.Contains(s, x) {
				todos = false
			}
		}
		if todos {
			return s
		}
		time.Sleep(30 * time.Millisecond)
	}
	p.t.Fatalf("no apareció %q en pantalla:\n%s", textos, s)
	return ""
}

func (p *prueba) tecla(r rune) { p.scr.InjectKey(tcell.KeyRune, r, tcell.ModNone) }
func (p *prueba) especial(k tcell.Key) {
	p.scr.InjectKey(k, 0, tcell.ModNone)
}
func (p *prueba) escribir(s string) {
	for _, r := range s {
		p.tecla(r)
	}
}

func TestFlujoCompleto(t *testing.T) {
	p, cfg := arrancar(t)

	// 1. Pantalla de impresoras.
	p.esperar("Impresoras (2, 2 habilitadas)", "[b4220000aa01]", "10.0.0.2", "sin lecturas", "Instaladas")

	// 2. Escanear: termina en el detalle del último escaneo.
	p.tecla('s')
	s := p.esperar("Detalle (O muestra OIDs crudos)", "Total: 128951", "Código 91 (no documentado)", "A4 / Carta")
	if strings.Contains(s, "Otro") {
		t.Error("el tamaño con 0 páginas no debió mostrarse")
	}
	filas, _, err := store.Leer(cfg.CSVAbs())
	if err != nil || len(filas) != 2 || filas[1].Estado != store.EstadoError {
		t.Fatalf("CSV: %d filas, %v", len(filas), err)
	}

	// 3. OIDs crudos.
	p.tecla('o')
	p.esperar("OIDs crudos", snmp.OIDSysDescr)

	// 4. Detalle de la impresora que falló.
	p.tecla('o')
	p.esperar("Detalle (O muestra")
	p.especial(tcell.KeyDown)
	p.esperar("Error:", "sin respuesta SNMP")

	// 5. Historial.
	p.tecla('4')
	p.esperar("1 escaneos, 2 filas", "Escaneo "+filas[0].EscaneoID)

	// 6. Volver a impresoras: muestra el estado de la lectura y desmarca una.
	p.tecla('1')
	p.esperar("ok ", "total 128951", "error ")
	p.tecla(' ')
	p.esperar("Impresoras (2, 1 habilitadas)")
	c2, _, err := config.Load(cfg.Path())
	if err != nil || c2.Printers[0].Habilitada {
		t.Fatalf("Espacio no guardó la config: %v %+v", err, c2.Printers)
	}

	// 7. Formulario de alta: validación de IP y cancelar con Esc.
	p.tecla('a')
	p.esperar("Agregar impresora por IP", "Probar", "Guardar")
	p.escribir("10.0.0.999")
	p.especial(tcell.KeyTab) // Nombre
	p.especial(tcell.KeyTab) // Marca
	p.especial(tcell.KeyTab) // Habilitada
	p.especial(tcell.KeyTab) // Probar
	p.especial(tcell.KeyEnter)
	p.esperar("no es una IPv4 válida")
	p.tecla('q') // en un formulario, Q no sale
	p.especial(tcell.KeyEscape)
	p.esperar("Impresoras (2, 1 habilitadas)")

	// 8. Alta válida con prueba: propone el nombre desde el modelo (sin hrDeviceDescr queda vacío).
	p.tecla('a')
	p.esperar("Agregar impresora por IP")
	p.escribir("10.0.0.1")
	for i := 0; i < 5; i++ {
		p.especial(tcell.KeyTab)
	}
	p.especial(tcell.KeyEnter) // Guardar → IP repetida
	p.esperar("ya está registrada")
	p.especial(tcell.KeyEscape)

	// 9. Eliminar con confirmación.
	p.especial(tcell.KeyDown)
	p.especial(tcell.KeyDelete)
	p.esperar("¿Eliminar 10.0.0.2")
	p.especial(tcell.KeyEnter) // Sí
	p.esperar("Impresoras (1, 0 habilitadas)")

	// 10. Q sale.
	p.tecla('q')
	select {
	case err := <-p.fin:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Q no cerró la aplicación")
	}
}

func TestDiferenciaEnHistorial(t *testing.T) {
	p, cfg := arrancar(t)
	p.esperar("Impresoras (2, 2 habilitadas)")
	p.tecla('s')
	p.esperar("Detalle (O muestra")
	time.Sleep(1100 * time.Millisecond) // otro escaneo_id
	p.tecla('s')
	esperarFilas(t, cfg.CSVAbs(), 4)
	p.esperar("Detalle (O muestra")
	p.tecla('4')
	p.esperar("2 escaneos, 4 filas")
	p.tecla(' ')
	p.especial(tcell.KeyDown)
	p.tecla(' ')
	p.esperar("marcados: 2/2")
	p.tecla('d')
	p.esperar("Consumo del", "error en el escaneo")
}

func esperarFilas(t *testing.T, csv string, n int) {
	t.Helper()
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		if fs, _, _ := store.Leer(csv); len(fs) >= n {
			time.Sleep(100 * time.Millisecond) // deja que la UI termine de cambiar de pantalla
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("el CSV no llegó a %d filas", n)
}

// Con la configuración vacía, al arrancar busca las instaladas sola (antes esto
// se bloqueaba porque QueueUpdateDraw se llamaba antes de Run).
func TestArranqueSinImpresoras(t *testing.T) {
	cfg := config.Default()
	cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	dial := snmp.MockDialer(map[string]*snmp.Mock{
		"10.0.0.1": snmp.NewMock(snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h")),
	})
	scr := tcell.NewSimulationScreen("UTF-8")
	ui := New(cfg, counters.NuevoRegistro(brother.Driver{}), dial)
	ui.instaladas = func(context.Context, string) ([]discovery.Instalada, error) {
		return []discovery.Instalada{
			{Nombre: "Brother DCP-L5650DN series", IP: "10.0.0.1", Metodo: "WSD"},
			{Nombre: "Brother MFC vieja", IP: "10.0.0.67", Metodo: `WSD (dispositivo padre SWD\IPP)`},
			{Nombre: "Brother HL-1110", Error: "sin IP: el puerto USB001 no es de red"},
		}, nil
	}
	ui.App().SetScreen(scr)
	p := &prueba{t: t, ui: ui, scr: scr, fin: make(chan error, 1)}
	go func() { p.fin <- ui.Run() }()
	t.Cleanup(func() { ui.Detener(); <-p.fin })
	scr.SetSize(170, 40)

	p.esperar("Nuevas en la lista: 2", "10.0.0.1 (responde)", "10.0.0.67 NO RESPONDE", "USB001 no es de red")
	p.especial(tcell.KeyEnter)
	p.esperar("Impresoras (2, 2 habilitadas)", "instalada")
	c2, _, err := config.Load(cfg.Path())
	if err != nil || len(c2.Printers) != 2 || c2.Printers[0].Origen != config.OrigenInstalada {
		t.Fatalf("config guardada: %+v %v", c2, err)
	}
}
