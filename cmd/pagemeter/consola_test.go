package main

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/AEROGU/pagemeter/internal/config"
)

const (
	envConsolaHijo   = "PAGEMETER_TEST_CONSOLA"
	createNewConsole = 0x00000010
)

var procWriteConsoleInput = syscall.NewLazyDLL("kernel32.dll").NewProc("WriteConsoleInputW")

type registroConsola [20]byte // INPUT_RECORD

func registroTecla(abajo bool, vk, ch uint16) registroConsola {
	var r registroConsola
	binary.LittleEndian.PutUint16(r[0:], 1) // KEY_EVENT
	if abajo {
		binary.LittleEndian.PutUint32(r[4:], 1)
	}
	binary.LittleEndian.PutUint16(r[8:], 1)
	binary.LittleEndian.PutUint16(r[10:], vk)
	binary.LittleEndian.PutUint16(r[14:], ch)
	return r
}

func registroMouse(x, y int16, botones uint32) registroConsola {
	var r registroConsola
	binary.LittleEndian.PutUint16(r[0:], 2) // MOUSE_EVENT
	binary.LittleEndian.PutUint16(r[4:], uint16(x))
	binary.LittleEndian.PutUint16(r[6:], uint16(y))
	binary.LittleEndian.PutUint32(r[8:], botones)
	return r
}

func inyectar(t *testing.T, conin syscall.Handle, rs ...registroConsola) {
	var n uint32
	ok, _, err := procWriteConsoleInput.Call(uintptr(conin), uintptr(unsafe.Pointer(&rs[0])), uintptr(len(rs)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 {
		t.Errorf("WriteConsoleInput: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}

func pulsar(t *testing.T, conin syscall.Handle, vk, ch uint16) {
	inyectar(t, conin, registroTecla(true, vk, ch), registroTecla(false, vk, ch))
}

// usarConsolaReal corre dentro de una consola propia y opera la app como un usuario.
func usarConsolaReal(t *testing.T, ruta string) {
	conin, err := syscall.Open("CONIN$", syscall.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		// Clic en la casilla de la primera impresora.
		inyectar(t, conin, registroMouse(2, 3, 1), registroMouse(2, 3, 0))
		time.Sleep(150 * time.Millisecond)
		pulsar(t, conin, 'A', 'a')
		time.Sleep(200 * time.Millisecond)
		for _, c := range "10.9.9.7" {
			vk := uint16(c)
			if c == '.' {
				vk = 0xBE
			}
			pulsar(t, conin, vk, uint16(c))
		}
		for range 5 { // IP → Nombre → Marca → Habilitada → Probar → Guardar
			pulsar(t, conin, 0x09, 9)
		}
		pulsar(t, conin, 0x0D, 13)
		time.Sleep(300 * time.Millisecond)
		pulsar(t, conin, 'Q', 'q')
	}()
	if code := run([]string{"--config", ruta}, io.Discard, os.Stderr, nil); code != salidaOK {
		t.Errorf("la app salió con código %d", code)
	}
}

// Una pulsación real produce dos eventos en la consola (bajar y soltar). Este test
// falla si la app procesa ambos: la "a" que abre el formulario se colaría en la IP.
func TestConsolaRealTecladoYMouse(t *testing.T) {
	if ruta := os.Getenv(envConsolaHijo); ruta != "" {
		usarConsolaReal(t, ruta)
		return
	}
	c := configPrueba(t, "10.0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConsolaRealTecladoYMouse$")
	cmd.Env = append(os.Environ(), envConsolaHijo+"="+c.Path())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNewConsole}
	salida, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("la app no terminó bien: %v\n%s", err, salida)
	}
	g, _, err := config.Load(c.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Printers) != 2 || g.Printers[1].IP != "10.9.9.7" {
		t.Fatalf("impresoras guardadas: %+v", g.Printers)
	}
	if g.Printers[0].Habilitada {
		t.Error("el clic en la casilla no desmarcó la impresora")
	}
}
