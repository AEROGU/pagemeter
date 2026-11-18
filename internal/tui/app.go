// Package tui es la interfaz de consola (tview). Regla: todo el estado de la UI
// (incluida la configuración) se toca sólo desde la goroutine de tview; el I/O
// corre en goroutines que regresan con app.QueueUpdateDraw.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/discovery"
	"github.com/AEROGU/pagemeter/internal/escaneo"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/store"
)

// Nombres de páginas.
const (
	pgImpresoras = "impresoras"
	pgEscanear   = "escanear"
	pgDetalle    = "detalle"
	pgHistorial  = "historial"
	pgFormulario = "formulario"
	pgRed        = "red"
	pgModal      = "modal"
)

var pestanas = []struct{ tecla, pagina, titulo string }{
	{"1", pgImpresoras, "Impresoras"},
	{"2", pgEscanear, "Escanear"},
	{"3", pgDetalle, "Último escaneo"},
	{"4", pgHistorial, "Historial"},
}

var ayudas = map[string]string{
	pgImpresoras: "Espacio marcar · I instaladas · A agregar IP · R buscar en red · E editar · Supr eliminar · P probar · S escanear",
	pgEscanear:   "S escanear habilitadas · Esc cancelar escaneo",
	pgDetalle:    "↑↓ impresora · O OIDs crudos · S escanear",
	pgHistorial:  "Espacio/M marcar (2) · D diferencia · F filtrar · Tab cambiar panel · R recargar",
	pgFormulario: "Tab siguiente campo · Enter en botón · Esc cancelar",
	pgRed:        "Enter elegir red · Espacio marcar · G agregar marcadas · Esc volver/cancelar",
}

const ayudaGlobal = "1-4 pantallas · Q salir"

// UI es la aplicación.
type UI struct {
	app    *tview.Application
	pages  *tview.Pages
	header *tview.TextView
	status *tview.TextView
	footer *tview.TextView

	cfg  *config.Config
	reg  *counters.Registro
	dial snmp.Dialer

	ctx    context.Context
	cancel context.CancelFunc

	estado      map[string]string // IP → texto de la última lectura (con colores)
	ultimo      *escaneo.Resultado
	paginaPrinc string // última pestaña principal visitada

	imp  *pantallaImpresoras
	esc  *pantallaEscanear
	det  *pantallaDetalle
	hist *pantallaHistorial
	red  *pantallaRed

	buscandoInstaladas bool
	// instaladas lista las impresoras de Windows (reemplazable en tests).
	instaladas func(ctx context.Context, filtro string) ([]discovery.Instalada, error)
}

// New arma la interfaz. dial puede ser nil (SNMP real).
func New(cfg *config.Config, reg *counters.Registro, dial snmp.Dialer) *UI {
	if dial == nil {
		dial = snmp.Dial
	}
	u := &UI{
		app:        tview.NewApplication(),
		pages:      tview.NewPages(),
		header:     tview.NewTextView().SetDynamicColors(true),
		status:     tview.NewTextView().SetDynamicColors(true),
		footer:     tview.NewTextView().SetDynamicColors(true),
		cfg:        cfg,
		reg:        reg,
		dial:       dial,
		estado:     map[string]string{},
		instaladas: discovery.Instaladas,
	}
	u.ctx, u.cancel = context.WithCancel(context.Background())
	u.footer.SetBackgroundColor(tcell.ColorDarkBlue)

	u.imp = newPantallaImpresoras(u)
	u.esc = newPantallaEscanear(u)
	u.det = newPantallaDetalle(u)
	u.hist = newPantallaHistorial(u)
	u.red = newPantallaRed(u)

	u.pages.AddPage(pgImpresoras, u.imp.raiz, true, true)
	u.pages.AddPage(pgEscanear, u.esc.raiz, true, false)
	u.pages.AddPage(pgDetalle, u.det.raiz, true, false)
	u.pages.AddPage(pgHistorial, u.hist.raiz, true, false)
	u.pages.AddPage(pgRed, u.red.raiz, true, false)
	u.pages.SetChangedFunc(u.actualizarBarras)
	u.paginaPrinc = pgImpresoras

	raiz := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.header, 1, 0, false).
		AddItem(u.pages, 0, 1, true).
		AddItem(u.status, 1, 0, false).
		AddItem(u.footer, 1, 0, false)
	u.app.SetRoot(raiz, true).SetInputCapture(u.teclasGlobales)
	u.actualizarBarras()
	return u
}

// App expone la aplicación tview (para tests con pantalla simulada).
func (u *UI) App() *tview.Application { return u.app }

// Run arranca la interfaz y bloquea hasta salir.
func (u *UI) Run() error {
	defer u.cancel()
	u.alIniciar()
	return u.app.Run()
}

// Detener sale de la aplicación cancelando las operaciones en curso.
func (u *UI) Detener() {
	u.cancel()
	u.app.Stop()
}

func (u *UI) alIniciar() {
	csv := u.cfg.CSVAbs()
	go func() {
		filas, _, err := store.Leer(csv)
		u.app.QueueUpdateDraw(func() {
			if err != nil {
				u.aviso("[red]No se pudo leer el historial: %s", tview.Escape(err.Error()))
				return
			}
			for ip, f := range store.UltimaPorImpresora(filas) {
				if _, ya := u.estado[ip]; !ya {
					u.estado[ip] = textoEstadoFila(f)
				}
			}
			u.imp.refrescar()
		})
	}()
	if len(u.cfg.Printers) == 0 {
		// En goroutine: QueueUpdate bloquea hasta que el loop de tview esté corriendo.
		go u.app.QueueUpdateDraw(func() { u.imp.buscarInstaladas() })
	}
}

func (u *UI) teclasGlobales(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		u.Detener()
		return nil
	}
	if u.escribiendo() {
		return ev
	}
	if ev.Key() == tcell.KeyRune {
		switch r := ev.Rune(); r {
		case 'q', 'Q':
			u.Detener()
			return nil
		case '1', '2', '3', '4':
			u.ir(pestanas[r-'1'].pagina)
			return nil
		}
	}
	return ev
}

// escribiendo indica si el foco está en algo donde las letras son texto
// (campos de formulario o un cuadro de diálogo).
func (u *UI) escribiendo() bool {
	if fp, _ := u.pages.GetFrontPage(); fp == pgModal || fp == pgFormulario {
		return true
	}
	switch u.app.GetFocus().(type) {
	case *tview.InputField, *tview.DropDown, *tview.Button, *tview.Checkbox:
		return true
	}
	return false
}

// ir cambia de pantalla.
func (u *UI) ir(pagina string) {
	if u.pages.HasPage(pgModal) {
		u.pages.RemovePage(pgModal)
	}
	if u.pages.HasPage(pgFormulario) {
		u.pages.RemovePage(pgFormulario)
	}
	for _, p := range pestanas {
		if p.pagina == pagina {
			u.paginaPrinc = pagina
		}
	}
	u.pages.SwitchToPage(pagina)
	switch pagina {
	case pgImpresoras:
		u.imp.refrescar()
	case pgDetalle:
		u.det.refrescar()
	case pgHistorial:
		u.hist.cargar()
	}
	u.app.SetFocus(u.pages)
	u.actualizarBarras()
}

func (u *UI) actualizarBarras() {
	front, _ := u.pages.GetFrontPage()
	var b strings.Builder
	b.WriteString("[::b]pagemeter[::-] ")
	for _, p := range pestanas {
		if p.pagina == u.paginaPrinc {
			fmt.Fprintf(&b, " [black:aqua] %s %s [-:-:-]", p.tecla, p.titulo)
		} else {
			fmt.Fprintf(&b, "  [aqua]%s[-] %s ", p.tecla, p.titulo)
		}
	}
	u.header.SetText(b.String())
	ayuda := ayudas[front]
	if front == pgModal {
		ayuda = "Enter aceptar · Tab/←→ cambiar botón"
	}
	if front != pgFormulario && front != pgModal {
		ayuda += " · " + ayudaGlobal
	}
	u.footer.SetText(" " + ayuda)
}

// aviso pone un mensaje en la línea de estado (acepta etiquetas de color).
func (u *UI) aviso(formato string, args ...any) {
	u.status.SetText(" " + fmt.Sprintf(formato, args...))
}

// mensaje muestra un cuadro ancho, con texto alineado a la izquierda y
// desplazable (↑↓ RePág AvPág), y un botón Aceptar.
func (u *UI) mensaje(texto string) {
	const ancho = 96
	tv := tview.NewTextView().SetText(texto).SetWrap(true).SetWordWrap(true).SetScrollable(true)
	btn := tview.NewButton("Aceptar")
	cerrar := func() {
		u.pages.RemovePage(pgModal)
		u.app.SetFocus(u.pages)
		u.actualizarBarras()
	}
	btn.SetSelectedFunc(cerrar)
	btn.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEscape:
			cerrar()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
			if h := tv.InputHandler(); h != nil {
				h(ev, func(tview.Primitive) {})
			}
			return nil
		}
		return ev
	})
	alto := 0
	for _, l := range strings.Split(texto, "\n") {
		alto += 1 + len([]rune(l))/(ancho-4)
	}
	alto = min(alto+4, 30)
	caja := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, false).
		AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(btn, 11, 0, true).AddItem(nil, 0, 1, false), 1, 0, true)
	caja.SetBorder(true).SetTitle(" Aviso ").SetBorderPadding(0, 0, 1, 1)
	centro := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(caja, alto, 0, true).
			AddItem(nil, 0, 1, false), ancho, 0, true).
		AddItem(nil, 0, 1, false)
	if u.pages.HasPage(pgModal) {
		u.pages.RemovePage(pgModal)
	}
	u.pages.AddPage(pgModal, centro, true, true)
	u.app.SetFocus(btn)
	u.actualizarBarras()
}

// confirmar pregunta Sí/No y llama si() con Sí.
func (u *UI) confirmar(texto string, si func()) {
	u.dialogo(texto, []string{"Sí", "No"}, func(i int) {
		if i == 0 {
			si()
		}
	})
}

func (u *UI) dialogo(texto string, botones []string, fn func(int)) {
	m := tview.NewModal().SetText(texto).AddButtons(botones)
	m.SetDoneFunc(func(i int, _ string) {
		u.pages.RemovePage(pgModal)
		u.app.SetFocus(u.pages)
		u.actualizarBarras()
		if fn != nil && i >= 0 {
			fn(i)
		}
	})
	if u.pages.HasPage(pgModal) {
		u.pages.RemovePage(pgModal)
	}
	u.pages.AddPage(pgModal, m, false, true)
	u.app.SetFocus(m)
	u.actualizarBarras()
}

// guardar escribe config.json; si falla, lo muestra.
func (u *UI) guardar() bool {
	if err := u.cfg.Save(); err != nil {
		u.mensaje("No se pudo guardar la configuración:\n" + err.Error())
		return false
	}
	return true
}

// marcas devuelve las opciones de marca para formularios: "auto" y los drivers.
func (u *UI) marcas() []string {
	out := []string{counters.MarcaAuto}
	for _, d := range u.reg.Drivers() {
		out = append(out, d.Marca())
	}
	return out
}

func textoEstadoFila(f store.Fila) string {
	cuando := f.Fecha.Format("02/01 15:04")
	if f.Estado != store.EstadoOK {
		return fmt.Sprintf("[red]error %s[-] %s", cuando, tview.Escape(recortar(f.Error, 60)))
	}
	t := "sin total"
	if v := f.Num[store.ColTotal]; v != nil {
		t = fmt.Sprintf("total %d", *v)
	}
	extra := ""
	if f.Error != "" {
		extra = " [yellow](con avisos)[-]"
	}
	return fmt.Sprintf("[green]ok %s[-] · %s%s", cuando, t, extra)
}

func textoEstadoLectura(r counters.Reading, err error, t time.Time) string {
	return textoEstadoFila(store.DesdeLectura(t, "", "", "", r, err))
}

func recortar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func fmtNum(v *int64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(*v)
}

// celda crea una celda de tabla con texto escapado.
func celda(s string) *tview.TableCell { return tview.NewTableCell(tview.Escape(s)) }

// encabezado agrega la fila 0 de una tabla.
func encabezado(t *tview.Table, cols ...string) {
	for i, c := range cols {
		t.SetCell(0, i, tview.NewTableCell(c).SetTextColor(tcell.ColorYellow).
			SetAttributes(tcell.AttrBold).SetSelectable(false))
	}
}

func esTecla(ev *tcell.EventKey, letras string) bool {
	return ev.Key() == tcell.KeyRune && strings.ContainsRune(letras, ev.Rune())
}
