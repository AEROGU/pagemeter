package tui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/escaneo"
)

type pantallaEscanear struct {
	u        *UI
	tabla    *tview.Table
	raiz     tview.Primitive
	cancelar context.CancelFunc
}

func newPantallaEscanear(u *UI) *pantallaEscanear {
	p := &pantallaEscanear{u: u, tabla: tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' ')}
	p.tabla.SetBorder(true).SetTitle(" Escanear ")
	p.tabla.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case esTecla(ev, "sS"):
			p.iniciar()
		case ev.Key() == tcell.KeyEscape:
			if p.cancelar != nil {
				p.cancelar()
				u.aviso("[yellow]Cancelando escaneo…[-]")
			}
		default:
			return ev
		}
		return nil
	})
	encabezado(p.tabla, "Impresora", "IP", "Estado", "Total")
	p.tabla.SetCell(1, 0, tview.NewTableCell("[gray]Pulsa S para leer las impresoras habilitadas.").SetSelectable(false))
	p.raiz = p.tabla
	return p
}

func (p *pantallaEscanear) iniciar() {
	u := p.u
	if p.cancelar != nil {
		return
	}
	cfg := u.cfg.Clone()
	ps := cfg.Habilitadas()
	if len(ps) == 0 {
		u.mensaje("No hay impresoras habilitadas.\nMárcalas con Espacio en la pantalla Impresoras.")
		return
	}
	p.tabla.Clear()
	encabezado(p.tabla, "Impresora", "IP", "Estado", "Total")
	for i, pr := range ps {
		p.tabla.SetCell(i+1, 0, celda(pr.Etiqueta()).SetExpansion(1))
		p.tabla.SetCell(i+1, 1, celda(pr.IP))
		p.tabla.SetCell(i+1, 2, celda(escaneo.EstadoPendiente).SetTextColor(tcell.ColorGray))
		p.tabla.SetCell(i+1, 3, celda(""))
	}
	p.tabla.Select(1, 0)
	p.tabla.SetTitle(fmt.Sprintf(" Escaneando %d impresoras… (Esc cancela) ", len(ps)))
	u.aviso("[yellow]Escaneando…[-]")

	ctx, cancel := context.WithCancel(u.ctx)
	p.cancelar = cancel
	go func() {
		res := escaneo.Ejecutar(ctx, cfg, u.reg, u.dial, func(i int, estado string, err error) {
			u.app.QueueUpdateDraw(func() {
				c := celda(estado)
				switch estado {
				case escaneo.EstadoLeyendo:
					c.SetTextColor(tcell.ColorYellow)
				case escaneo.EstadoOK:
					c.SetTextColor(tcell.ColorGreen)
				case escaneo.EstadoError:
					c = celda("error: " + recortar(errTexto(err), 70)).SetTextColor(tcell.ColorRed)
				default:
					c.SetTextColor(tcell.ColorGray)
				}
				p.tabla.SetCell(i+1, 2, c)
			})
		})
		u.app.QueueUpdateDraw(func() { p.terminar(res) })
	}()
}

func errTexto(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (p *pantallaEscanear) terminar(res escaneo.Resultado) {
	u := p.u
	p.cancelar = nil
	for i, l := range res.Lecturas {
		p.tabla.SetCell(i+1, 3, celda(fmtNum(l.Reading.Total)))
		u.estado[l.Printer.IP] = textoEstadoFila(l.Fila)
	}
	ok := len(res.Lecturas) - res.Fallidas()
	p.tabla.SetTitle(fmt.Sprintf(" Escaneo %s: %d ok, %d con error ", res.ID, ok, res.Fallidas()))
	u.imp.refrescar()
	if res.CSVErr != nil {
		u.aviso("[red]El CSV no se actualizó[-]")
		u.mensaje(fmt.Sprintf("Lecturas: %d ok, %d con error.\n\n%v", ok, res.Fallidas(), res.CSVErr))
		if u.ctx.Err() == nil && len(res.Lecturas) > 0 {
			u.ultimo = &res
		}
		return
	}
	u.ultimo = &res
	u.aviso("[green]Escaneo %s guardado en %s[-]", res.ID, tview.Escape(res.CSVPath))
	u.ir(pgDetalle)
}
