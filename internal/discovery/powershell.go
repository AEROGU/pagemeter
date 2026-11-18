// Package discovery encuentra impresoras: las instaladas en Windows (vía PowerShell)
// y las que responden SNMP en una red.
package discovery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const createNoWindow = 0x08000000

// RunPowerShell ejecuta un script en Windows PowerShell sin ventana visible y
// devuelve su salida estándar (UTF-8). El script viaja con -EncodedCommand, así
// que no hay problemas de comillas.
func RunPowerShell(ctx context.Context, script string) ([]byte, error) {
	full := "$ProgressPreference='SilentlyContinue'\n" +
		"[Console]::OutputEncoding=[System.Text.Encoding]::UTF8\n" + script
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodeCommand(full))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(errb.String())
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return nil, fmt.Errorf("ejecutar PowerShell: %w: %s", err, msg)
	}
	return bytes.TrimPrefix(out.Bytes(), []byte{0xEF, 0xBB, 0xBF}), nil
}

// encodeCommand codifica en base64 UTF-16LE, como espera -EncodedCommand.
func encodeCommand(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Lista acepta lo que produce ConvertTo-Json: un arreglo, un objeto suelto
// (cuando hay un solo elemento), null o nada.
type Lista[T any] []T

func (l *Lista[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*l = nil
		return nil
	}
	if b[0] == '[' {
		var s []T
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*l = s
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*l = Lista[T]{v}
	return nil
}

// Texto acepta una cadena, un número, un booleano o null y lo guarda como texto.
// Get-PnpDeviceProperty devuelve datos de tipos variados.
type Texto string

func (t *Texto) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*t = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Texto(s)
		return nil
	}
	*t = Texto(b) // número, bool u objeto: se guarda tal cual
	return nil
}
