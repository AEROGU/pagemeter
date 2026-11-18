package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/store"
)

type pantallaHistorial struct {
	u        *UI
	filtro   *tview.InputField
	escaneos *tview.Table
	detalle  *tview.Table
	raiz     tview.Primitive

	todos    []store.Escaneo
	vista    []store.Escaneo // con filtro aplicado
	marcados []string        // IDs (máximo 2, en orden de marcado)
	cargando bool
}

var columnasDetalle = []string{"Total", "PC", "PC color", "PC mono", "Copia", "Copia color", "Copia mono", "Fax", "Fax color", "Fax mono"}

func newPantallaHistorial(u *UI) *pantallaHistorial {
	p := &pantallaHistorial{
		u:        u,
		filtro:   tview.NewInputField().SetLabel("Filtrar impresora (nombre o IP): ").SetFieldWidth(30),
		escaneos: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' '),
		detalle:  tview.NewTable().SetSelectable(true, false).SetFixed(1, 1).SetSeparator('│'),
	}
	p.filtro.SetChangedFunc(func(string) { p.aplicarFiltro() })
	p.filtro.SetDoneFunc(func(tcell.Key) { u.app.SetFocus(p.escaneos) })
	p.escaneos.SetBorder(true).SetTitle(" Escaneos ")
	p.detalle.SetBorder(true)
	p.escaneos.SetSelectionChangedFunc(func(row, _ int) { p.mostrarEscaneo(row - 1) })

	captura := func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyTab:
			if u.app.GetFocus() == p.escaneos {
				u.app.SetFocus(p.detalle)
			} else {
				u.app.SetFocus(p.escaneos)
			}
		case esTecla(ev, "fF/"):
			u.app.SetFocus(p.filtro)
		case esTecla(ev, "rR"):
			p.cargar()
		case esTecla(ev, "dD"):
			p.diferencia()
		case ev.Key() == tcell.KeyRune && (ev.Rune() == ' ' || ev.Rune() == 'm' || ev.Rune() == 'M'):
			if u.app.GetFocus() == p.escaneos {
				p.marcar()
			}
		default:
			return ev
		}
		return nil
	}
	p.escaneos.SetInputCapture(captura)
	p.detalle.SetInputCapture(captura)

	p.raiz = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filtro, 1, 0, false).
		AddItem(tview.NewFlex().
			AddItem(p.escaneos, 42, 0, true).
			AddItem(p.detalle, 0, 1, false), 0, 1, true)
	return p
}

// cargar lee el CSV completo en segundo plano.
func (p *pantallaHistorial) cargar() {
	u := p.u
	if p.cargando {
		return
	}
	p.cargando = true
	path := u.cfg.CSVAbs()
	u.aviso("[yellow]Cargando %s…[-]", tview.Escape(path))
	go func() {
		filas, avisos, err := store.Leer(path)
		u.app.QueueUpdateDraw(func() {
			p.cargando = false
			if err != nil {
				u.aviso("[red]%s[-]", tview.Escape(err.Error()))
				return
			}
			p.todos = store.Agrupar(filas)
			msg := fmt.Sprintf("[green]%d escaneos, %d filas[-] en %s", len(p.todos), len(filas), tview.Escape(path))
			if len(avisos) > 0 {
				msg += fmt.Sprintf(" · [yellow]%d líneas ignoradas (la primera: %s)[-]", len(avisos), tview.Escape(avisos[0]))
			}
			u.aviso("%s", msg)
			p.aplicarFiltro()
		})
	}()
}

func (p *pantallaHistorial) aplicarFiltro() {
	p.vista = store.FiltrarImpresora(p.todos, p.filtro.GetText())
	p.pintarEscaneos()
}

func (p *pantallaHistorial) marcado(id string) int {
	for i, m := range p.marcados {
		if m == id {
			return i
		}
	}
	return -1
}

func (p *pantallaHistorial) pintarEscaneos() {
	row, _ := p.escaneos.GetSelection()
	p.escaneos.Clear()
	encabezado(p.escaneos, " ", "Fecha", "Impresoras")
	for i, e := range p.vista {
		m := " "
		if k := p.marcado(e.ID); k >= 0 {
			m = []string{"A", "B"}[k]
		}
		imp := fmt.Sprintf("%d ok", e.OK())
		if e.Errores() > 0 {
			imp += fmt.Sprintf(", [red]%d error[-]", e.Errores())
		}
		p.escaneos.SetCell(i+1, 0, celda(m).SetTextColor(tcell.ColorAqua))
		p.escaneos.SetCell(i+1, 1, celda(e.Fecha.Format(store.FormatoFecha)))
		p.escaneos.SetCell(i+1, 2, tview.NewTableCell(imp))
	}
	if len(p.vista) == 0 {
		p.escaneos.SetCell(1, 1, tview.NewTableCell("[gray]Sin escaneos.").SetSelectable(false))
		p.escaneos.Select(0, 0)
		p.detalle.Clear()
		p.detalle.SetTitle(" Detalle ")
		return
	}
	if row < 1 || row > len(p.vista) {
		row = 1
	}
	p.escaneos.Select(row, 0)
	p.escaneos.SetTitle(fmt.Sprintf(" Escaneos (%d) · marcados: %d/2 ", len(p.vista), len(p.marcados)))
	p.mostrarEscaneo(row - 1)
}

func (p *pantallaHistorial) mostrarEscaneo(i int) {
	if i < 0 || i >= len(p.vista) {
		return
	}
	e := p.vista[i]
	t := p.detalle
	t.Clear()
	encabezado(t, append([]string{"Impresora", "IP", "Estado"}, columnasDetalle...)...)
	for r, f := range e.Filas {
		est := celda(f.Estado).SetTextColor(tcell.ColorGreen)
		if f.Estado != store.EstadoOK {
			est = celda("error").SetTextColor(tcell.ColorRed)
		}
		t.SetCell(r+1, 0, celda(recortar(f.Impresora, 32)))
		t.SetCell(r+1, 1, celda(f.IP))
		t.SetCell(r+1, 2, est)
		for c := range columnasDetalle {
			t.SetCell(r+1, 3+c, celda(numFila(f, c)).SetAlign(tview.AlignRight))
		}
	}
	t.Select(1, 0)
	t.ScrollToBeginning()
	t.SetTitle(fmt.Sprintf(" Escaneo %s · %s ", e.ID, e.Fecha.Format(store.FormatoFecha)))
}

func (p *pantallaHistorial) marcar() {
	row, _ := p.escaneos.GetSelection()
	i := row - 1
	if i < 0 || i >= len(p.vista) {
		return
	}
	id := p.vista[i].ID
	if k := p.marcado(id); k >= 0 {
		p.marcados = append(p.marcados[:k], p.marcados[k+1:]...)
	} else {
		p.marcados = append(p.marcados, id)
		if len(p.marcados) > 2 {
			p.marcados = p.marcados[1:]
		}
	}
	p.pintarEscaneos()
	if len(p.marcados) == 2 {
		p.u.aviso("Dos escaneos marcados: pulsa D para ver la diferencia.")
	}
}

func (p *pantallaHistorial) buscar(id string) (store.Escaneo, bool) {
	for _, e := range p.vista {
		if e.ID == id {
			return e, true
		}
	}
	return store.Escaneo{}, false
}

func (p *pantallaHistorial) diferencia() {
	if len(p.marcados) != 2 {
		p.u.mensaje("Marca dos escaneos con Espacio (o M) en la lista de la izquierda y pulsa D.")
		return
	}
	a, okA := p.buscar(p.marcados[0])
	b, okB := p.buscar(p.marcados[1])
	if !okA || !okB {
		p.u.mensaje("Uno de los escaneos marcados no está visible con el filtro actual.")
		return
	}
	antes, despues, difs := store.Diferencia(a, b)
	t := p.detalle
	t.Clear()
	// La nota va antes de los números para que se vea sin desplazarse a la derecha.
	encabezado(t, append([]string{"Impresora", "IP", "Nota"}, columnasDetalle...)...)
	for r, d := range difs {
		t.SetCell(r+1, 0, celda(recortar(d.Impresora, 32)))
		t.SetCell(r+1, 1, celda(d.IP))
		t.SetCell(r+1, 2, celda(recortar(d.Nota, 34)).SetTextColor(tcell.ColorYellow))
		for c := range columnasDetalle {
			txt := fmtNum(d.Delta[c])
			cel := celda(txt).SetAlign(tview.AlignRight)
			if d.Delta[c] != nil && *d.Delta[c] < 0 {
				cel.SetTextColor(tcell.ColorRed)
			}
			t.SetCell(r+1, 3+c, cel)
		}
	}
	t.Select(1, 0)
	t.ScrollToBeginning()
	t.SetTitle(fmt.Sprintf(" Consumo del %s al %s ", antes.Fecha.Format(store.FormatoFecha), despues.Fecha.Format(store.FormatoFecha)))
	p.u.app.SetFocus(t)
}
