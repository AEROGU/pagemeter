package tui

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/discovery"
)

type resultadoRed struct {
	discovery.Respuesta
	marcada    bool
	registrada bool
	candidata  bool
}

type pantallaRed struct {
	u          *UI
	redes      *tview.List
	rango      *tview.InputField
	progreso   *tview.TextView
	tabla      *tview.Table
	raiz       *tview.Flex
	interfaces []discovery.Interfaz
	res        []resultadoRed
	cancelar   context.CancelFunc
}

func newPantallaRed(u *UI) *pantallaRed {
	p := &pantallaRed{
		u:        u,
		redes:    tview.NewList().ShowSecondaryText(false),
		rango:    tview.NewInputField().SetLabel("Rango CIDR: ").SetFieldWidth(20),
		progreso: tview.NewTextView().SetDynamicColors(true),
		tabla:    tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' '),
	}
	p.redes.SetBorder(true).SetTitle(" 1) Elige la red a escanear ")
	p.redes.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEscape {
			p.salir()
			return nil
		}
		return ev
	})
	p.rango.SetPlaceholder("192.168.1.0/24")
	p.rango.SetDoneFunc(func(k tcell.Key) {
		switch k {
		case tcell.KeyEnter:
			p.escanearTexto(p.rango.GetText())
		case tcell.KeyEscape:
			u.app.SetFocus(p.redes)
		}
	})
	p.tabla.SetBorder(true).SetTitle(" 2) Resultados ")
	p.tabla.SetInputCapture(p.teclasTabla)

	p.raiz = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().
			AddItem(p.redes, 0, 1, true).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(p.rango, 1, 0, false).
				AddItem(p.progreso, 0, 1, false), 0, 1, false), 9, 0, true).
		AddItem(p.tabla, 0, 1, false)
	return p
}

// abrir prepara la pantalla: lista interfaces y deja el foco en la lista.
func (p *pantallaRed) abrir() {
	p.redes.Clear()
	ifs, err := discovery.Interfaces()
	p.interfaces = ifs
	if err != nil {
		p.progreso.SetText("[red]" + tview.Escape(err.Error()))
	}
	for _, i := range ifs {
		i := i
		txt := fmt.Sprintf("%-22s %-15s %s", recortar(i.Nombre, 22), i.IP, i.CIDR())
		p.redes.AddItem(tview.Escape(txt), "", 0, func() { p.escanearInterfaz(i) })
	}
	p.redes.AddItem("Capturar un rango a mano…", "", 0, func() { p.u.app.SetFocus(p.rango) })
	if p.cancelar == nil && len(p.res) == 0 {
		p.progreso.SetText(fmt.Sprintf("[gray]Elige una interfaz o captura un rango (/%d a /%d).\nSe pregunta sysDescr por SNMP a cada dirección.",
			discovery.PrefijoMin, discovery.PrefijoMax))
		p.pintarTabla()
	}
	p.u.app.SetFocus(p.redes)
}

func (p *pantallaRed) salir() {
	if p.cancelar != nil {
		p.cancelar()
		return
	}
	p.u.ir(pgImpresoras)
}

func (p *pantallaRed) escanearInterfaz(i discovery.Interfaz) {
	if pre := i.Prefijo(); pre < discovery.PrefijoMin || pre > discovery.PrefijoMax {
		p.progreso.SetText(fmt.Sprintf("[red]La red %s es /%d; se admite de /%d a /%d. Captura un rango más chico a mano.",
			i.CIDR(), pre, discovery.PrefijoMin, discovery.PrefijoMax))
		return
	}
	p.escanear(i.Red, i.IP.String())
}

func (p *pantallaRed) escanearTexto(s string) {
	red, err := discovery.ParseCIDR(s)
	if err != nil {
		p.progreso.SetText("[red]" + tview.Escape(err.Error()))
		return
	}
	// Si el rango cae en una interfaz conocida, se enlaza a su IP.
	local := ""
	for _, i := range p.interfaces {
		if i.Red.Contains(red.IP) || red.Contains(i.IP) {
			local = i.IP.String()
		}
	}
	p.escanear(red, local)
}

func (p *pantallaRed) escanear(red *net.IPNet, localIP string) {
	if p.cancelar != nil {
		return
	}
	hosts, err := discovery.Hosts(red)
	if err != nil {
		p.progreso.SetText("[red]" + tview.Escape(err.Error()))
		return
	}
	u := p.u
	ctx, cancel := context.WithCancel(u.ctx)
	p.cancelar = cancel
	p.res = nil
	p.pintarTabla()
	enlace := "sin enlazar a una interfaz"
	if localIP != "" {
		enlace = "desde " + localIP
	}
	p.progreso.SetText(fmt.Sprintf("[yellow]Escaneando %s (%d direcciones, %s)…[-]\nEsc cancela.", red, len(hosts), enlace))
	u.app.SetFocus(p.tabla)

	cfg := u.cfg.Clone()
	ini := time.Now()
	var ultimoPintado time.Time
	go func() {
		res, err := discovery.Barrer(ctx, hosts, discovery.OpcionesBarrido{
			Base:         cfg.Params(""),
			LocalIP:      localIP,
			Timeout:      time.Duration(cfg.RedTimeoutMs) * time.Millisecond,
			Concurrencia: cfg.RedConcurrencia,
			Dialer:       u.dial,
			Candidata:    func(sd string) bool { return u.reg.Reconoce(sd) || coincideFiltro(sd, cfg.Filtro) },
			Progreso: func(h, total int) {
				// Se llama en serie; se limita el redibujado a ~10 por segundo.
				if h != total && time.Since(ultimoPintado) < 100*time.Millisecond {
					return
				}
				ultimoPintado = time.Now()
				u.app.QueueUpdateDraw(func() {
					p.progreso.SetText(fmt.Sprintf("[yellow]Escaneando %s: %d/%d (%d%%)…[-]\nEsc cancela.", red, h, total, h*100/total))
				})
			},
		})
		u.app.QueueUpdateDraw(func() {
			p.cancelar = nil
			p.cargarResultados(res, cfg.Filtro)
			dur := time.Since(ini).Round(100 * time.Millisecond)
			if err != nil {
				p.progreso.SetText(fmt.Sprintf("[yellow]Escaneo cancelado tras %v.[-] Respondieron %d.", dur, len(res)))
			} else {
				p.progreso.SetText(fmt.Sprintf("[green]Listo en %v:[-] respondieron %d de %d direcciones.\nMarca con Espacio y pulsa G para agregar.", dur, len(res), len(hosts)))
			}
		})
	}()
}

func coincideFiltro(sd, filtro string) bool {
	return filtro != "" && strings.Contains(strings.ToLower(sd), strings.ToLower(filtro))
}

func (p *pantallaRed) cargarResultados(rs []discovery.Respuesta, filtro string) {
	p.res = p.res[:0]
	for _, r := range rs {
		x := resultadoRed{Respuesta: r}
		x.registrada = p.u.cfg.Buscar(r.IP) >= 0
		x.candidata = p.u.reg.Reconoce(r.SysDescr) || coincideFiltro(r.SysDescr, filtro)
		x.marcada = x.candidata && !x.registrada
		p.res = append(p.res, x)
	}
	// Candidatas primero.
	var cand, resto []resultadoRed
	for _, x := range p.res {
		if x.candidata {
			cand = append(cand, x)
		} else {
			resto = append(resto, x)
		}
	}
	p.res = append(cand, resto...)
	p.pintarTabla()
}

func (p *pantallaRed) pintarTabla() {
	row, _ := p.tabla.GetSelection()
	p.tabla.Clear()
	encabezado(p.tabla, "   ", "IP", "Modelo", "sysDescr", "")
	for i, x := range p.res {
		r := i + 1
		casilla := "[ ]"
		if x.marcada {
			casilla = "[x]"
		}
		nota := ""
		color := tcell.ColorWhite
		switch {
		case x.registrada:
			nota, color = "ya registrada", tcell.ColorGray
			casilla = " - "
		case x.candidata:
			nota, color = "candidata", tcell.ColorGreen
		default:
			color = tcell.ColorGray
		}
		p.tabla.SetCell(r, 0, celda(casilla).SetTextColor(tcell.ColorAqua))
		p.tabla.SetCell(r, 1, celda(x.IP).SetTextColor(color))
		p.tabla.SetCell(r, 2, celda(x.Modelo).SetTextColor(color))
		p.tabla.SetCell(r, 3, celda(recortar(x.SysDescr, 60)).SetTextColor(color).SetExpansion(1))
		p.tabla.SetCell(r, 4, celda(nota).SetTextColor(color))
	}
	if len(p.res) == 0 {
		p.tabla.SetCell(1, 2, tview.NewTableCell("[gray]Sin resultados todavía.").SetSelectable(false))
		p.tabla.Select(0, 0)
	} else if row < 1 || row > len(p.res) {
		p.tabla.Select(1, 0)
	}
	p.tabla.SetTitle(fmt.Sprintf(" 2) Resultados (%d, %d marcadas) ", len(p.res), p.marcadas()))
}

func (p *pantallaRed) marcadas() int {
	n := 0
	for _, x := range p.res {
		if x.marcada {
			n++
		}
	}
	return n
}

func (p *pantallaRed) teclasTabla(ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case ev.Key() == tcell.KeyEscape:
		if p.cancelar != nil {
			p.cancelar()
		} else {
			p.u.app.SetFocus(p.redes)
		}
	case ev.Key() == tcell.KeyRune && ev.Rune() == ' ':
		row, _ := p.tabla.GetSelection()
		if i := row - 1; i >= 0 && i < len(p.res) && !p.res[i].registrada {
			p.res[i].marcada = !p.res[i].marcada
			p.pintarTabla()
		}
	case esTecla(ev, "gG"), ev.Key() == tcell.KeyEnter:
		p.agregar()
	default:
		return ev
	}
	return nil
}

func (p *pantallaRed) agregar() {
	u := p.u
	if p.cancelar != nil {
		return
	}
	respaldo := u.cfg.Clone().Printers
	var nombres []string
	for _, x := range p.res {
		if !x.marcada || x.registrada {
			continue
		}
		nombre := x.Modelo
		if nombre == "" {
			nombre = strings.TrimSpace(strings.SplitN(x.SysDescr, ",", 2)[0])
		}
		err := u.cfg.Agregar(config.Printer{Nombre: nombre, IP: x.IP, Origen: config.OrigenRed, Habilitada: true})
		if err == nil {
			nombres = append(nombres, fmt.Sprintf("• %s (%s)", nombre, x.IP))
		}
	}
	if len(nombres) == 0 {
		u.mensaje("No hay impresoras marcadas para agregar.")
		return
	}
	if !u.guardar() {
		u.cfg.Printers = respaldo
		return
	}
	p.res = nil
	u.ir(pgImpresoras)
	u.mensaje(fmt.Sprintf("Se agregaron %d impresoras:\n\n%s", len(nombres), strings.Join(nombres, "\n")))
}
