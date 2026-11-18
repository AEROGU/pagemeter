package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/escaneo"
	"github.com/AEROGU/pagemeter/internal/store"
)

type pantallaDetalle struct {
	u      *UI
	lista  *tview.List
	texto  *tview.TextView
	raiz   tview.Primitive
	crudos bool
}

func newPantallaDetalle(u *UI) *pantallaDetalle {
	p := &pantallaDetalle{
		u:     u,
		lista: tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true),
		texto: tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(false),
	}
	p.lista.SetBorder(true).SetTitle(" Impresoras ")
	p.texto.SetBorder(true)
	p.lista.SetChangedFunc(func(i int, _, _ string, _ rune) { p.mostrar(i) })
	captura := func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case esTecla(ev, "oO"):
			p.crudos = !p.crudos
			p.mostrar(p.lista.GetCurrentItem())
		case esTecla(ev, "sS"):
			u.ir(pgEscanear)
			u.esc.iniciar()
		case ev.Key() == tcell.KeyTab:
			if u.app.GetFocus() == p.lista {
				u.app.SetFocus(p.texto)
			} else {
				u.app.SetFocus(p.lista)
			}
		default:
			return ev
		}
		return nil
	}
	p.lista.SetInputCapture(captura)
	p.texto.SetInputCapture(captura)
	p.raiz = tview.NewFlex().AddItem(p.lista, 40, 0, true).AddItem(p.texto, 0, 1, false)
	return p
}

func (p *pantallaDetalle) refrescar() {
	actual := p.lista.GetCurrentItem()
	p.lista.Clear()
	if p.u.ultimo == nil {
		p.texto.SetTitle(" Detalle ")
		p.texto.SetText("[gray]Todavía no hay un escaneo en esta sesión. Pulsa S para escanear.\n\n" +
			"El historial completo (de este y otros días) está en la pantalla 4.")
		return
	}
	for _, l := range p.u.ultimo.Lecturas {
		marca := "[green]●[-] "
		if l.Err != nil {
			marca = "[red]●[-] "
		}
		p.lista.AddItem(marca+tview.Escape(recortar(l.Printer.Etiqueta(), 34)), "", 0, nil)
	}
	if actual >= 0 && actual < p.lista.GetItemCount() {
		p.lista.SetCurrentItem(actual)
	}
	p.lista.SetTitle(fmt.Sprintf(" Escaneo %s ", p.u.ultimo.ID))
	p.mostrar(p.lista.GetCurrentItem())
}

func (p *pantallaDetalle) mostrar(i int) {
	if p.u.ultimo == nil || i < 0 || i >= len(p.u.ultimo.Lecturas) {
		return
	}
	l := p.u.ultimo.Lecturas[i]
	if p.crudos {
		p.texto.SetTitle(" OIDs crudos (O vuelve al detalle) ")
		p.texto.SetText(textoCrudos(l))
	} else {
		p.texto.SetTitle(" Detalle (O muestra OIDs crudos) ")
		p.texto.SetText(textoDetalle(l))
	}
	p.texto.ScrollToBeginning()
}

func textoDetalle(l escaneo.Lectura) string {
	r := l.Reading
	e := tview.Escape
	var b strings.Builder
	fmt.Fprintf(&b, "[::b]%s[::-]  (%s)\n", e(l.Printer.Etiqueta()), l.Printer.IP)
	if r.SysDescr != "" {
		fmt.Fprintf(&b, "[gray]sysDescr:[-] %s\n", e(r.SysDescr))
	}
	if r.Marca != "" {
		fmt.Fprintf(&b, "[gray]Driver:[-] %s\n", r.Marca)
	}
	if l.Err != nil {
		fmt.Fprintf(&b, "\n[red]Error:[-] %s\n", e(l.Err.Error()))
		return b.String()
	}
	fmt.Fprintf(&b, "\n[::b]Total:[::-] %s\n", valorONo(r.Total))

	fila := func(nombre string, v *int64) {
		if v != nil {
			fmt.Fprintf(&b, "  %-14s %10d\n", nombre, *v)
		}
	}
	b.WriteString("\n[yellow]Resumen[-]\n")
	fila("PC total", r.PC.Total)
	fila("PC color", r.PC.Color)
	fila("PC mono", r.PC.Mono)
	fila("Copia total", r.Copia.Total)
	fila("Copia color", r.Copia.Color)
	fila("Copia mono", r.Copia.Mono)
	fila("Fax total", r.Fax.Total)
	fila("Fax color", r.Fax.Color)
	fila("Fax mono", r.Fax.Mono)

	if len(r.Funciones) > 0 {
		b.WriteString("\n[yellow]Por función[-] (códigos sin documentar en [darkorange]naranja[-])\n")
		lineas(&b, r.Funciones, false)
	}
	if len(r.Tamanos) > 0 {
		b.WriteString("\n[yellow]Por tamaño de papel[-] (se omiten los que valen 0)\n")
		lineas(&b, r.Tamanos, true)
	}
	if len(r.Avisos) > 0 {
		b.WriteString("\n[yellow]Avisos[-]\n")
		for _, a := range r.Avisos {
			fmt.Fprintf(&b, "  %s\n", e(a))
		}
	}
	return b.String()
}

func lineas(b *strings.Builder, ls []counters.Linea, omitirCeros bool) {
	for _, x := range ls {
		if omitirCeros && x.Paginas == 0 {
			continue
		}
		color := ""
		if !x.Conocido {
			color = "[darkorange]"
		}
		fmt.Fprintf(b, "  %s%-32s %10d[-]\n", color, tview.Escape(x.Nombre), x.Paginas)
	}
}

func valorONo(v *int64) string {
	if v == nil {
		return "[gray]no lo reporta[-]"
	}
	return fmt.Sprint(*v)
}

func textoCrudos(l escaneo.Lectura) string {
	if len(l.Reading.Crudos) == 0 {
		return "[gray]No hay OIDs leídos."
	}
	var b strings.Builder
	for _, v := range l.Reading.Crudos {
		fmt.Fprintf(&b, "%-50s %s\n", v.OID, tview.Escape(v.String()))
	}
	return b.String()
}

// usado por el historial para mostrar números.
func numFila(f store.Fila, i int) string { return fmtNum(f.Num[i]) }
