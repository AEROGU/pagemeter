package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/store"
)

func configPrueba(t *testing.T, ips ...string) *config.Config {
	t.Helper()
	c := config.Default()
	c.SetPath(filepath.Join(t.TempDir(), "config.json"))
	for _, ip := range ips {
		if err := c.Agregar(config.Printer{Nombre: "Imp " + ip, IP: ip, Origen: config.OrigenManual, Habilitada: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return c
}

func dialPrueba() snmp.Dialer {
	return snmp.MockDialer(map[string]*snmp.Mock{
		"10.0.0.1": snmp.NewMock(
			snmp.StrVar(snmp.OIDSysDescr, "Brother NC-8900h"),
			snmp.IntVar(snmp.OIDPrtMarkerLifeCount+".1.1", 500),
		),
	})
}

func correr(dial snmp.Dialer, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, dial)
	return code, out.String(), errOut.String()
}

func TestScanTodoBien(t *testing.T) {
	c := configPrueba(t, "10.0.0.1")
	code, out, errOut := correr(dialPrueba(), "--scan", "--config", c.Path())
	if code != salidaOK || errOut != "" {
		t.Fatalf("código %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "1 ok, 0 con error") || !strings.Contains(out, "total 500") || !strings.Contains(out, c.CSVAbs()) {
		t.Errorf("salida:\n%s", out)
	}
	// La marca sin tabla Brother deja un aviso, pero la lectura cuenta como ok.
	if !strings.Contains(out, "aviso:") {
		t.Errorf("falta el aviso:\n%s", out)
	}
	if fs, _, err := store.Leer(c.CSVAbs()); err != nil || len(fs) != 1 {
		t.Errorf("CSV: %d filas, %v", len(fs), err)
	}
}

func TestScanConImpresoraFallida(t *testing.T) {
	c := configPrueba(t, "10.0.0.1", "10.0.0.2")
	code, out, _ := correr(dialPrueba(), "--scan", "--config", c.Path())
	if code != salidaFallas {
		t.Fatalf("código %d", code)
	}
	if !strings.Contains(out, "1 ok, 1 con error") || !strings.Contains(out, "error  Imp 10.0.0.2") {
		t.Errorf("salida:\n%s", out)
	}
	// La fila de la impresora que falló también se escribe.
	fs, _, err := store.Leer(c.CSVAbs())
	if err != nil || len(fs) != 2 || fs[1].Estado != store.EstadoError {
		t.Errorf("CSV: %+v, %v", fs, err)
	}
}

func TestScanSinHabilitadas(t *testing.T) {
	c := configPrueba(t)
	code, _, errOut := correr(dialPrueba(), "--scan", "--config", c.Path())
	if code != salidaGeneral || !strings.Contains(errOut, "no hay impresoras habilitadas") {
		t.Fatalf("código %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(c.CSVAbs()); !os.IsNotExist(err) {
		t.Error("no debió crear el CSV")
	}
}

func TestScanCSVIncompatible(t *testing.T) {
	c := configPrueba(t, "10.0.0.1")
	if err := os.WriteFile(c.CSVAbs(), []byte("otro,formato\r\n1,2\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := correr(dialPrueba(), "--scan", "--config", c.Path())
	if code != salidaGeneral || !strings.Contains(errOut, "encabezado") {
		t.Fatalf("código %d, stderr %q", code, errOut)
	}
}

func TestArgumentosYConfigInvalidos(t *testing.T) {
	if code, _, _ := correr(dialPrueba(), "--no-existe"); code != salidaGeneral {
		t.Errorf("bandera desconocida: %d", code)
	}
	if code, _, errOut := correr(dialPrueba(), "--scan", "sobra"); code != salidaGeneral || !strings.Contains(errOut, "sobra") {
		t.Errorf("argumento sobrante: %d %q", code, errOut)
	}
	if code, _, errOut := correr(dialPrueba(), "-h"); code != salidaOK || !strings.Contains(errOut, "Uso: pagemeter") {
		t.Errorf("ayuda: %d %q", code, errOut)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte("{roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := correr(dialPrueba(), "--scan", "--config", p); code != salidaGeneral || !strings.Contains(errOut, "JSON") {
		t.Errorf("config rota: %d %q", code, errOut)
	}
}
