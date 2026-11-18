package tui

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
)

// UsarConsolaWindows hace que la interfaz lea el teclado y el mouse con la API de
// consola de Windows. Debe llamarse antes de Run.
//
// La pantalla por defecto de tcell (v2.13) no sirve en la consola de Windows 10:
// lee también los eventos de soltar la tecla, así que cada pulsación llega dos veces
// y Esc y las flechas se reciben mal. La pantalla de consola sí los descarta.
func (u *UI) UsarConsolaWindows() error {
	// SetScreen no informa si falla la inicialización, así que se comprueba antes.
	for _, nombre := range []string{"CONIN$", "CONOUT$"} {
		f, err := os.OpenFile(nombre, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("la interfaz necesita una consola de Windows: %w", err)
		}
		f.Close()
	}
	s, err := tcell.NewConsoleScreen()
	if err != nil {
		return fmt.Errorf("abrir la consola: %w", err)
	}
	u.app.SetScreen(s)
	// Run sólo activa el mouse en las pantallas que crea él mismo.
	s.EnableMouse()
	return nil
}
