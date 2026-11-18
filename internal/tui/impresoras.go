package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/discovery"
	"github.com/AEROGU/pagemeter/internal/escaneo"
)

type pantallaImpresoras struct {
	u     *UI
	tabla *tview.Table
	raiz  tview.Primitive
}

func newPantallaImpresoras(u *UI) *pantallaImpresoras {
	p := &pantallaImpresoras{u: u, tabla: tview.NewTable()}
	p.tabla.SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' ')
	p.tabla.SetBorder(true).SetTitle(" Impresoras ")
	p.tabla.SetInputCapture(p.teclas)
	p.tabla.SetSelectedFunc(func(int, int) { p.editar() })
	p.raiz = p.tabla
	p.refrescar()
	return p
}

// seleccionada devuelve la impresora de la fila actual.
func (p *pantallaImpresoras) seleccionada() (config.Printer, bool) {
	row, _ := p.tabla.GetSelection()
	i := row - 1
	if i < 0 || i >= len(p.u.cfg.Printers) {
		return config.Printer{}, false
	}
	return p.u.cfg.Printers[i], true
}

func (p *pantallaImpresoras) refrescar() {
	row, _ := p.tabla.GetSelection()
	p.tabla.Clear()
	encabezado(p.tabla, "   ", "Nombre", "IP", "Marca", "Origen", "Última lectura")
	for i, pr := range p.u.cfg.Printers {
		casilla := "[ ]"
		if pr.Habilitada {
			casilla = "[x]"
		}
		r := i + 1
		p.tabla.SetCell(r, 0, celda(casilla).SetTextColor(tcell.ColorAqua).SetClickedFunc(func() bool {
			p.tabla.Select(r, 0)
			p.alternar()
			return true
		}))
		p.tabla.SetCell(r, 1, celda(pr.Etiqueta()).SetExpansion(1))
		p.tabla.SetCell(r, 2, celda(pr.IP))
		p.tabla.SetCell(r, 3, celda(pr.Marca))
		p.tabla.SetCell(r, 4, celda(pr.Origen))
		est, ok := p.u.estado[pr.IP]
		if !ok {
			est = "[gray]sin lecturas[-]"
		}
		p.tabla.SetCell(r, 5, tview.NewTableCell(est))
	}
	if len(p.u.cfg.Printers) == 0 {
		p.tabla.SetCell(1, 1, tview.NewTableCell("[gray]No hay impresoras. Pulsa I (instaladas), A (por IP) o R (buscar en la red).").SetSelectable(false))
	}
	n := len(p.u.cfg.Printers)
	switch {
	case n == 0:
		p.tabla.Select(0, 0)
	case row < 1:
		p.tabla.Select(1, 0)
	case row > n:
		p.tabla.Select(n, 0)
	default:
		p.tabla.Select(row, 0)
	}
	p.tabla.SetTitle(fmt.Sprintf(" Impresoras (%d, %d habilitadas) ", n, len(p.u.cfg.Habilitadas())))
}

func (p *pantallaImpresoras) teclas(ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case ev.Key() == tcell.KeyRune && ev.Rune() == ' ':
		p.alternar()
	case ev.Key() == tcell.KeyDelete:
		p.eliminar()
	case esTecla(ev, "iI"):
		p.buscarInstaladas()
	case esTecla(ev, "aA"):
		p.u.abrirFormulario("")
	case esTecla(ev, "rR"):
		p.u.ir(pgRed)
		p.u.red.abrir()
	case esTecla(ev, "eE"):
		p.editar()
	case esTecla(ev, "pP"):
		p.probar()
	case esTecla(ev, "sS"):
		p.u.ir(pgEscanear)
		p.u.esc.iniciar()
	default:
		return ev
	}
	return nil
}

func (p *pantallaImpresoras) alternar() {
	pr, ok := p.seleccionada()
	if !ok {
		return
	}
	_ = p.u.cfg.SetHabilitada(pr.IP, !pr.Habilitada)
	if !p.u.guardar() {
		_ = p.u.cfg.SetHabilitada(pr.IP, pr.Habilitada)
	}
	p.refrescar()
}

func (p *pantallaImpresoras) editar() {
	if pr, ok := p.seleccionada(); ok {
		p.u.abrirFormulario(pr.IP)
	}
}

func (p *pantallaImpresoras) eliminar() {
	pr, ok := p.seleccionada()
	if !ok {
		return
	}
	p.u.confirmar(fmt.Sprintf("¿Eliminar %s (%s) de la lista?\n\nEl historial del CSV no se borra.", pr.Etiqueta(), pr.IP), func() {
		respaldo := p.u.cfg.Clone().Printers
		p.u.cfg.Eliminar(pr.IP)
		if !p.u.guardar() {
			p.u.cfg.Printers = respaldo
		}
		delete(p.u.estado, pr.IP)
		p.refrescar()
	})
}

func (p *pantallaImpresoras) probar() {
	pr, ok := p.seleccionada()
	if !ok {
		return
	}
	u := p.u
	u.estado[pr.IP] = "[yellow]probando…[-]"
	p.refrescar()
	cfg, ctx := u.cfg.Clone(), u.ctx
	go func() {
		res, err := escaneo.Probar(ctx, cfg, u.reg, u.dial, pr.IP, pr.Marca)
		u.app.QueueUpdateDraw(func() {
			u.estado[pr.IP] = textoEstadoLectura(res.Reading, err, time.Now())
			p.refrescar()
			if err != nil {
				u.mensaje(fmt.Sprintf("%s (%s)\n\nNo respondió:\n%v", pr.Etiqueta(), pr.IP, err))
				return
			}
			u.mensaje(textoPrueba(pr.Etiqueta(), pr.IP, res))
		})
	}()
}

func textoPrueba(nombre, ip string, res escaneo.Prueba) string {
	r := res.Reading
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) responde\n\n", nombre, ip)
	fmt.Fprintf(&b, "sysDescr: %s\n", r.SysDescr)
	if res.Modelo != "" {
		fmt.Fprintf(&b, "Modelo: %s\n", res.Modelo)
	}
	fmt.Fprintf(&b, "Driver: %s\n", r.Marca)
	if r.Total != nil {
		fmt.Fprintf(&b, "Total: %d páginas\n", *r.Total)
	} else {
		b.WriteString("Total: no lo reporta\n")
	}
	for _, a := range r.Avisos {
		fmt.Fprintf(&b, "Aviso: %s\n", a)
	}
	return b.String()
}

// buscarInstaladas relee las impresoras instaladas en Windows, las verifica por
// SNMP y agrega las nuevas (sin duplicar por IP).
func (p *pantallaImpresoras) buscarInstaladas() {
	u := p.u
	if u.buscandoInstaladas {
		return
	}
	u.buscandoInstaladas = true
	u.aviso("[yellow]Buscando impresoras instaladas en Windows…[-]")
	cfg, ctx := u.cfg.Clone(), u.ctx
	go func() {
		ins, err := u.instaladas(ctx, cfg.Filtro)
		if err == nil {
			discovery.Verificar(ctx, ins, cfg.Params(""), u.dial)
		}
		u.app.QueueUpdateDraw(func() {
			u.buscandoInstaladas = false
			if err != nil {
				u.aviso("")
				u.mensaje("No se pudieron leer las impresoras instaladas:\n" + err.Error())
				return
			}
			p.fusionar(ins)
		})
	}()
}

func (p *pantallaImpresoras) fusionar(ins []discovery.Instalada) {
	u := p.u
	var nuevas []config.Printer
	for _, in := range ins {
		if in.IP != "" {
			nuevas = append(nuevas, config.Printer{Nombre: in.Nombre, IP: in.IP})
		}
	}
	respaldo := u.cfg.Clone().Printers
	n := u.cfg.FusionarInstaladas(nuevas)
	if n > 0 && !u.guardar() {
		u.cfg.Printers = respaldo
		n = 0
	}
	p.refrescar()

	var b strings.Builder
	filtro := u.cfg.Filtro
	if filtro == "" {
		filtro = "(sin filtro)"
	}
	fmt.Fprintf(&b, "Instaladas que coinciden con %q: %d. Nuevas en la lista: %d.\n\n", filtro, len(ins), n)
	for _, in := range ins {
		switch {
		case in.IP == "":
			fmt.Fprintf(&b, "• %s — %s\n", in.Nombre, in.Error)
		case in.Responde:
			fmt.Fprintf(&b, "• %s — %s (responde)\n", in.Nombre, in.IP)
		default:
			fmt.Fprintf(&b, "• %s — %s NO RESPONDE (%s; la IP puede estar vieja: edítala o usa R)\n", in.Nombre, in.IP, in.Metodo)
		}
	}
	if len(ins) == 0 {
		b.WriteString("No se encontraron impresoras instaladas con ese filtro.")
	}
	u.aviso("")
	u.mensaje(b.String())
}
