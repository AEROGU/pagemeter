package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/AEROGU/pagemeter/internal/config"
)

// filas devuelve la pantalla como runas, una por celda.
func (p *prueba) filas() ([][]rune, []tcell.SimCell, int) {
	type foto struct {
		c []tcell.SimCell
		w int
	}
	ch := make(chan foto, 1)
	p.ui.App().QueueUpdate(func() {
		c, w, _ := p.scr.GetContents()
		ch <- foto{append([]tcell.SimCell(nil), c...), w}
	})
	f := <-ch
	var out [][]rune
	for i := 0; i+f.w <= len(f.c); i += f.w {
		fila := make([]rune, f.w)
		for j := range fila {
			fila[j] = ' '
			if r := f.c[i+j].Runes; len(r) > 0 {
				fila[j] = r[0]
			}
		}
		out = append(out, fila)
	}
	return out, f.c, f.w
}

// ubicar busca un texto desde abajo y devuelve su posición.
func (p *prueba) ubicar(texto string) (int, int) {
	p.t.Helper()
	p.esperar(texto)
	buscado := []rune(texto)
	fs, _, _ := p.filas()
	for y := len(fs) - 1; y >= 0; y-- {
		for x := 0; x+len(buscado) <= len(fs[y]); x++ {
			if string(fs[y][x:x+len(buscado)]) == texto {
				return x, y
			}
		}
	}
	p.t.Fatalf("no se ubicó %q", texto)
	return 0, 0
}

func (p *prueba) clic(x, y int) {
	p.scr.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	p.scr.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	time.Sleep(30 * time.Millisecond)
}

func (p *prueba) clicEn(texto string) {
	p.t.Helper()
	x, y := p.ubicar(texto)
	p.clic(x, y)
}

func TestTeclasConFondoAmarillo(t *testing.T) {
	p, _ := arrancar(t)
	for _, c := range []struct {
		tecla, texto string
	}{{"I", "Instaladas"}, {"A", "Agregar IP"}, {"R", "Buscar en red"}, {"E", "Editar"}, {"Supr", "Eliminar"}, {"Espacio", "Marcar"}, {"Q", "Salir"}} {
		x, y := p.ubicar(" " + c.tecla + "  " + c.texto)
		_, celdas, w := p.filas()
		_, bg, _ := celdas[y*w+x+1].Style.Decompose()
		if bg != tcell.ColorYellow {
			t.Errorf("%s: fondo de la tecla = %v", c.tecla, bg)
		}
		_, bg, _ = celdas[y*w+x+len([]rune(c.tecla))+3].Style.Decompose()
		if bg != tcell.ColorDarkBlue {
			t.Errorf("%s: fondo del texto = %v", c.texto, bg)
		}
	}
}

func TestBarraAngostaUsaVariasFilas(t *testing.T) {
	p, _ := arrancar(t)
	p.scr.SetSize(70, 30)
	p.ui.App().QueueUpdateDraw(func() {})
	s := p.esperar("Marcar", "Instaladas", "Buscar en red", "Eliminar", "Escanear", "Salir")
	if strings.Contains(s, "panic") {
		t.Fatal(s)
	}
	// Los comandos de la segunda fila también responden al clic.
	p.clicEn("Salir")
	select {
	case <-p.fin:
	case <-time.After(3 * time.Second):
		t.Fatal("el clic en Salir no cerró la aplicación")
	}
}

func TestMouse(t *testing.T) {
	p, cfg := arrancar(t)
	p.esperar("Impresoras (2, 2 habilitadas)")

	// Clic en la casilla: desmarca y guarda.
	x, y := p.ubicar("[x] 10.0.0.2")
	p.clic(x+1, y)
	p.esperar("Impresoras (2, 1 habilitadas)")
	c2, _, err := config.Load(cfg.Path())
	if err != nil || c2.Printers[1].Habilitada {
		t.Fatalf("no se guardó: %v %+v", err, c2.Printers)
	}

	// Clic en una fila: la selecciona, sin cambiar casillas.
	p.clicEn("Brother DCP-L5650DN")
	p.esperar("Impresoras (2, 1 habilitadas)")

	// Con un diálogo abierto, un clic afuera no cambia la selección de atrás.
	p.clicEn("Eliminar")
	p.esperar("¿Eliminar Brother DCP-L5650DN")
	_, yFila := p.ubicar("[ ] 10.0.0.2")
	p.clic(20, yFila)
	p.esperar("¿Eliminar Brother DCP-L5650DN")
	p.clicEn("Sí")
	p.esperar("Impresoras (1, 0 habilitadas)", "10.0.0.2")

	// Comando con clic y formulario: cancelar desde la barra.
	p.clicEn("Agregar IP")
	p.esperar("Agregar impresora por IP")
	p.clicEn("Cancelar")
	p.esperar("Impresoras (1, 0 habilitadas)")

	// Pestañas con clic.
	p.clicEn("4 Historial")
	p.esperar("Filtrar impresora")
	p.clicEn("1 Impresoras")
	p.esperar("Impresoras (1, 0 habilitadas)")
}

func TestMouseHistorial(t *testing.T) {
	p, cfg := arrancar(t)
	p.esperar("Impresoras (2, 2 habilitadas)")
	p.clicEn("Escanear  ")
	esperarFilas(t, cfg.CSVAbs(), 2)
	p.esperar("Detalle (O muestra")
	time.Sleep(1100 * time.Millisecond)
	p.clicEn("Escanear  ")
	esperarFilas(t, cfg.CSVAbs(), 4)
	p.esperar("Detalle (O muestra")
	p.clicEn("4 Historial")
	p.esperar("2 escaneos, 4 filas")

	// Con el foco en el filtro, un comando actúa en vez de escribirse.
	p.clicEn("Filtrar impresora")
	p.escribir("q1") // tras un clic en el campo, las teclas son texto: no salen ni cambian de pantalla
	p.esperar("(nombre o IP): q1")
	p.especial(tcell.KeyBackspace2)
	p.especial(tcell.KeyBackspace2)
	p.clicEn("Recargar")
	p.esperar("2 escaneos, 4 filas")

	x, y := p.ubicar("[ ] 20")
	p.clic(x+1, y)
	p.esperar("[A] 20")
	x, y = p.ubicar("[ ] 20")
	p.clic(x+1, y)
	p.esperar("marcados: 2/2", "[B] 20")
	p.clicEn("Diferencia")
	p.esperar("Consumo del", "error en el escaneo")
}
