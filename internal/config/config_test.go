package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ejemplo = `{
  "community": "public",
  "snmp_version": "1",
  "timeout_ms": 2000,
  "retries": 1,
  "filtro": "Brother",
  "csv_path": "contadores.csv",
  "printers": [
    {"nombre": "Brother DCP-L5650DN series", "ip": "192.168.1.104", "origen": "instalada", "habilitada": true},
    {"ip": "192.168.1.178", "origen": "manual", "habilitada": false}
  ]
}`

func escribir(t *testing.T, contenido string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEjemplo(t *testing.T) {
	p := escribir(t, ejemplo)
	c, creado, err := Load(p)
	if err != nil || creado {
		t.Fatalf("Load: %v, creado=%v", err, creado)
	}
	if len(c.Printers) != 2 || c.Printers[0].IP != "192.168.1.104" || !c.Printers[0].Habilitada {
		t.Fatalf("printers = %+v", c.Printers)
	}
	// Valores que el ejemplo no trae toman el defecto.
	if c.RedTimeoutMs != 800 || c.RedConcurrencia != 64 || c.Printers[1].Marca != "auto" {
		t.Errorf("defectos no aplicados: %+v", c)
	}
	if c.Printers[1].Etiqueta() != "192.168.1.178" {
		t.Errorf("etiqueta sin nombre = %q", c.Printers[1].Etiqueta())
	}
	if got := c.CSVAbs(); got != filepath.Join(filepath.Dir(p), "contadores.csv") {
		t.Errorf("CSVAbs = %s", got)
	}
	if hab := c.Habilitadas(); len(hab) != 1 || hab[0].IP != "192.168.1.104" {
		t.Errorf("habilitadas = %+v", hab)
	}
	prm := c.Params("10.0.0.1")
	if prm.Timeout != 2*time.Second || prm.Community != "public" || prm.Version != "1" {
		t.Errorf("params = %+v", prm)
	}
}

func TestLoadConBOM(t *testing.T) {
	p := escribir(t, "\uFEFF"+ejemplo)
	if _, _, err := Load(p); err != nil {
		t.Fatalf("BOM: %v", err)
	}
}

func TestLoadNoExiste(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, creado, err := Load(p)
	if err != nil || !creado {
		t.Fatalf("Load: %v, creado=%v", err, creado)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("Load no debe crear el archivo")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, creado, err := Load(p)
	if err != nil || creado || c2.Community != "public" || c2.Printers == nil {
		t.Fatalf("relectura: %+v, %v, %v", c2, creado, err)
	}
}

func TestLoadInvalidos(t *testing.T) {
	cases := map[string]string{
		"json roto":   `{"printers": [`,
		"version":     `{"snmp_version": "3"}`,
		"ip":          `{"printers": [{"ip": "192.168.1.300", "origen": "manual"}]}`,
		"hostname":    `{"printers": [{"ip": "BRWAABBCCDDEE01", "origen": "manual"}]}`,
		"origen":      `{"printers": [{"ip": "10.0.0.1", "origen": "otro"}]}`,
		"ip repetida": `{"printers": [{"ip": "10.0.0.1", "origen": "manual"}, {"ip": "10.0.0.1", "origen": "red"}]}`,
	}
	for nombre, js := range cases {
		if _, _, err := Load(escribir(t, js)); err == nil {
			t.Errorf("%s: se esperaba error", nombre)
		}
	}
}

func TestSaveAtomicoYReemplaza(t *testing.T) {
	p := escribir(t, ejemplo)
	c, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetHabilitada("192.168.1.178", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	// No deben quedar temporales.
	ents, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	c2, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c2.Printers[1].Habilitada {
		t.Error("el cambio no se guardó")
	}
	// Un Save inválido no debe tocar el archivo.
	antes, _ := os.ReadFile(p)
	c2.Printers = append(c2.Printers, Printer{IP: "192.168.1.104", Origen: OrigenRed})
	if err := c2.Save(); err == nil {
		t.Error("Save con IP repetida debió fallar")
	}
	despues, _ := os.ReadFile(p)
	if string(antes) != string(despues) {
		t.Error("un Save fallido modificó el archivo")
	}
}

func TestCSVAbsoluto(t *testing.T) {
	c := Default()
	c.SetPath(`C:\x\config.json`)
	c.CSVPath = `D:\datos\cont.csv`
	if c.CSVAbs() != `D:\datos\cont.csv` {
		t.Errorf("CSVAbs = %s", c.CSVAbs())
	}
}

func TestOperacionesPrinters(t *testing.T) {
	c := Default()
	if err := c.Agregar(Printer{IP: "10.0.0.1", Origen: OrigenManual}); err != nil {
		t.Fatal(err)
	}
	if err := c.Agregar(Printer{IP: " 10.0.0.1 ", Origen: OrigenRed}); err == nil {
		t.Error("IP duplicada debió fallar")
	}
	if err := c.Agregar(Printer{IP: "abc", Origen: OrigenManual}); err == nil {
		t.Error("IP inválida debió fallar")
	}
	if err := c.Agregar(Printer{IP: "10.0.0.2", Nombre: "B", Origen: OrigenManual}); err != nil {
		t.Fatal(err)
	}
	if err := c.Editar("10.0.0.1", Printer{IP: "10.0.0.2", Origen: OrigenManual}); err == nil {
		t.Error("editar hacia IP existente debió fallar")
	}
	if err := c.Editar("10.0.0.1", Printer{IP: "10.0.0.3", Nombre: "A", Origen: OrigenManual}); err != nil {
		t.Fatal(err)
	}
	if c.Buscar("10.0.0.3") != 0 || c.Buscar("10.0.0.1") != -1 {
		t.Error("Editar no cambió la IP")
	}
	n := c.FusionarInstaladas([]Printer{
		{IP: "10.0.0.2", Nombre: "Instalada que ya existía"},
		{IP: "10.0.0.9", Nombre: "Nueva"},
		{IP: "10.0.0.9", Nombre: "Otra cola de la misma"},
	})
	if n != 1 || c.Printers[1].Nombre != "B" {
		t.Errorf("fusión: n=%d, printers=%+v", n, c.Printers)
	}
	if p := c.Printers[2]; p.Origen != OrigenInstalada || !p.Habilitada || p.Nombre != "Nueva" {
		t.Errorf("instalada agregada = %+v", p)
	}
	if !c.Eliminar("10.0.0.2") || c.Eliminar("10.0.0.2") || len(c.Printers) != 2 {
		t.Error("Eliminar")
	}
}
