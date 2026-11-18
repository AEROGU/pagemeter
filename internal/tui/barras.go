package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// comando es una entrada de la barra inferior. Con el mouse, un clic equivale a
// pulsar su tecla; los que tienen key == 0 y r == 0 son sólo informativos.
type comando struct {
	tecla, texto string
	key          tcell.Key
	r            rune
	foco         func(u *UI) // deja el foco donde la tecla tiene efecto
}

func letra(tecla, texto string) comando {
	return comando{tecla: tecla, texto: texto, key: tcell.KeyRune, r: []rune(strings.ToLower(tecla))[0]}
}

func especial(tecla, texto string, key tcell.Key) comando {
	return comando{tecla: tecla, texto: texto, key: key}
}

func espacio(texto string) comando {
	return comando{tecla: "Espacio", texto: texto, key: tcell.KeyRune, r: ' '}
}

func (c comando) conFoco(f func(u *UI)) comando {
	c.foco = f
	return c
}

func focoTablaRed(u *UI) { u.app.SetFocus(u.red.tabla) }
func focoListaRed(u *UI) { u.app.SetFocus(u.red.redes) }
func focoEscaneos(u *UI) { u.app.SetFocus(u.hist.escaneos) }

var comandos = map[string][]comando{
	pgImpresoras: {
		espacio("Marcar"), letra("I", "Instaladas"), letra("A", "Agregar IP"), letra("R", "Buscar en red"),
		letra("E", "Editar"), especial("Supr", "Eliminar", tcell.KeyDelete), letra("P", "Probar"), letra("S", "Escanear"),
	},
	pgEscanear: {letra("S", "Escanear"), especial("Esc", "Cancelar", tcell.KeyEscape)},
	pgDetalle: {
		{tecla: "↑↓", texto: "Impresora"}, letra("O", "OIDs crudos"),
		especial("Tab", "Cambiar panel", tcell.KeyTab), letra("S", "Escanear"),
	},
	pgHistorial: {
		espacio("Marcar").conFoco(focoEscaneos), letra("D", "Diferencia"), letra("F", "Filtrar"),
		especial("Tab", "Cambiar panel", tcell.KeyTab), letra("R", "Recargar"),
	},
	pgFormulario: {
		especial("Tab", "Siguiente campo", tcell.KeyTab), especial("Enter", "Activar", tcell.KeyEnter),
		especial("Esc", "Cancelar", tcell.KeyEscape),
	},
	pgRed: {
		especial("Enter", "Elegir red", tcell.KeyEnter).conFoco(focoListaRed),
		espacio("Marcar").conFoco(focoTablaRed), letra("G", "Agregar marcadas").conFoco(focoTablaRed),
		especial("Esc", "Volver/cancelar", tcell.KeyEscape),
	},
	pgModal: {
		especial("Enter", "Aceptar", tcell.KeyEnter), especial("Tab", "Cambiar botón", tcell.KeyTab),
		especial("Esc", "Cerrar", tcell.KeyEscape),
	},
}

var comandosGlobales = []comando{{tecla: "1-4", texto: "Pantallas"}, letra("Q", "Salir")}

// zona es un tramo clicable de una barra (x final excluida).
type zona struct {
	fila, x0, x1 int
	pagina       string  // pestaña del encabezado
	cmd          comando // comando de la barra inferior
}

func enRect(p tview.Primitive, x, y int) bool {
	rx, ry, w, h := p.GetRect()
	return x >= rx && x < rx+w && y >= ry && y < ry+h
}

func dialogoAbierto(front string) bool { return front == pgModal || front == pgFormulario }

// actualizarBarras redibuja el encabezado y la barra de comandos.
func (u *UI) actualizarBarras() {
	var b strings.Builder
	b.WriteString("[::b]pagemeter[::-] ")
	u.zonasHeader = u.zonasHeader[:0]
	for _, p := range pestanas {
		x0 := tview.TaggedStringWidth(b.String())
		if p.pagina == u.paginaPrinc {
			fmt.Fprintf(&b, " [black:aqua] %s %s [-:-:-]", p.tecla, p.titulo)
		} else {
			fmt.Fprintf(&b, "  [aqua]%s[-] %s ", p.tecla, p.titulo)
		}
		u.zonasHeader = append(u.zonasHeader, zona{x0: x0, x1: tview.TaggedStringWidth(b.String()), pagina: p.pagina})
	}
	u.header.SetText(b.String())
	u.pintarComandos()
}

// pintarComandos reparte los comandos en tantas filas como hagan falta para el
// ancho actual: cada tecla lleva fondo amarillo.
func (u *UI) pintarComandos() {
	front, _ := u.pages.GetFrontPage()
	cmds := append([]comando(nil), comandos[front]...)
	if !dialogoAbierto(front) {
		cmds = append(cmds, comandosGlobales...)
	}
	ancho := u.anchoBarra
	if ancho <= 0 {
		ancho = 120
	}
	u.zonasFooter = u.zonasFooter[:0]
	filas := []string{" "}
	x := 1
	for _, c := range cmds {
		wTecla := tview.TaggedStringWidth(c.tecla) + 2
		w := wTecla + 1 + tview.TaggedStringWidth(c.texto)
		if x > 1 && x+w > ancho {
			filas = append(filas, " ")
			x = 1
		}
		f := len(filas) - 1
		filas[f] += fmt.Sprintf("[black:yellow:b] %s [-:-:-] %s  ", c.tecla, c.texto)
		u.zonasFooter = append(u.zonasFooter, zona{fila: f, x0: x, x1: x + w, cmd: c})
		x += w + 2
	}
	u.footer.SetText(strings.Join(filas, "\n"))
	u.raiz.ResizeItem(u.footer, len(filas), 0)
}

// antesDeDibujar reacomoda la barra cuando cambia el ancho de la ventana.
func (u *UI) antesDeDibujar(s tcell.Screen) bool {
	if w, _ := s.Size(); w != u.anchoBarra {
		u.anchoBarra = w
		u.pintarComandos()
	}
	return false
}

// capturaMouse evita que, con un diálogo abierto, un clic fuera de él llegue a
// la pantalla de atrás y le quite el foco.
//
// Además convierte el doble clic en clic: tview llama doble clic a dos clics
// seguidos aunque sean en lugares distintos, y las tablas lo ignoran, así que
// marcar dos casillas rápido perdería la segunda.
func (u *UI) capturaMouse(ev *tcell.EventMouse, a tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if ev == nil || a == tview.MouseMove {
		return ev, a
	}
	if a == tview.MouseLeftDoubleClick {
		a = tview.MouseLeftClick
	}
	front, _ := u.pages.GetFrontPage()
	if dialogoAbierto(front) && u.dialogoActual != nil {
		x, y := ev.Position()
		if enRect(u.pages, x, y) && !enRect(u.dialogoActual, x, y) {
			return nil, a
		}
	}
	return ev, a
}

// capturaBarra hace que una barra no tome el foco y avisa de sus clics.
func capturaBarra(b *tview.TextView, clic func(x, y int)) func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	return func(a tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := ev.Position()
		if a == tview.MouseMove || !enRect(b, x, y) {
			return a, ev
		}
		if a == tview.MouseLeftClick && clic != nil {
			rx, ry, _, _ := b.GetRect()
			clic(x-rx, y-ry)
		}
		return tview.MouseConsumed, nil
	}
}

func (u *UI) clicHeader(x, _ int) {
	if front, _ := u.pages.GetFrontPage(); dialogoAbierto(front) {
		return
	}
	for _, z := range u.zonasHeader {
		if x >= z.x0 && x < z.x1 {
			u.ir(z.pagina)
			return
		}
	}
}

func (u *UI) clicFooter(x, y int) {
	for _, z := range u.zonasFooter {
		if z.fila == y && x >= z.x0 && x < z.x1 {
			u.ejecutar(z.cmd)
			return
		}
	}
}

// ejecutar manda la tecla del comando por el camino normal del teclado.
func (u *UI) ejecutar(c comando) {
	if c.key == 0 && c.r == 0 {
		return
	}
	front, _ := u.pages.GetFrontPage()
	if !dialogoAbierto(front) {
		// En un campo de texto la letra se escribiría en vez de actuar.
		switch u.app.GetFocus().(type) {
		case *tview.InputField, *tview.TextArea:
			u.app.SetFocus(u.pages)
		}
		if c.foco != nil {
			c.foco(u)
		}
	}
	u.app.QueueEvent(tcell.NewEventKey(c.key, c.r, tcell.ModNone))
}
