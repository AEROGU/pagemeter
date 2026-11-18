// Package config carga y guarda config.json (junto al .exe por defecto).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
)

// Orígenes válidos de una impresora.
const (
	OrigenInstalada = "instalada"
	OrigenManual    = "manual"
	OrigenRed       = "red"
)

// NombreArchivo es el nombre por defecto del archivo de configuración.
const NombreArchivo = "config.json"

// Printer es una impresora conocida.
type Printer struct {
	Nombre     string `json:"nombre,omitempty"`
	IP         string `json:"ip"`
	Marca      string `json:"marca,omitempty"` // "auto" (o vacío) = detectar por sysDescr
	Origen     string `json:"origen"`
	Habilitada bool   `json:"habilitada"`
}

// Etiqueta es el nombre a mostrar (la IP si no tiene nombre).
func (p Printer) Etiqueta() string {
	if strings.TrimSpace(p.Nombre) != "" {
		return p.Nombre
	}
	return p.IP
}

// Config es el contenido de config.json.
type Config struct {
	Community   string `json:"community"`
	SNMPVersion string `json:"snmp_version"`
	TimeoutMs   int    `json:"timeout_ms"`
	Retries     int    `json:"retries"`
	Filtro      string `json:"filtro"`
	CSVPath     string `json:"csv_path"`
	// Barrido de red: timeout por host y cuántos hosts a la vez.
	RedTimeoutMs    int       `json:"red_timeout_ms"`
	RedConcurrencia int       `json:"red_concurrencia"`
	Printers        []Printer `json:"printers"`

	path string // archivo del que se cargó / al que se guarda
}

// Default devuelve la configuración por defecto.
func Default() *Config {
	return &Config{
		Community:       "public",
		SNMPVersion:     "1",
		TimeoutMs:       2000,
		Retries:         1,
		Filtro:          "Brother",
		CSVPath:         "contadores.csv",
		RedTimeoutMs:    800,
		RedConcurrencia: 64,
		Printers:        []Printer{},
	}
}

// DefaultPath es config.json en la carpeta del ejecutable.
func DefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("ubicar el ejecutable: %w", err)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), NombreArchivo), nil
}

// Load lee la configuración. Si el archivo no existe, devuelve la configuración
// por defecto y creado=true (todavía no la escribe: eso lo decide quien llama).
func Load(path string) (cfg *Config, creado bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, false, fmt.Errorf("ruta de configuración %q: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		c := Default()
		c.path = abs
		return c, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("leer %s: %w", abs, err)
	}
	c := Default()
	c.Printers = nil
	data = trimBOM(data) // por si alguien la editó con el Bloc de notas
	if err := json.Unmarshal(data, c); err != nil {
		return nil, false, fmt.Errorf("%s no es JSON válido: %w", abs, err)
	}
	c.path = abs
	c.normalizar()
	if err := c.Validate(); err != nil {
		return nil, false, fmt.Errorf("%s: %w", abs, err)
	}
	return c, false, nil
}

// Path es el archivo asociado.
func (c *Config) Path() string { return c.path }

// SetPath cambia el archivo asociado (p. ej. para una config creada en memoria).
func (c *Config) SetPath(p string) { c.path = p }

// Dir es la carpeta de la configuración; las rutas relativas se resuelven contra ella.
func (c *Config) Dir() string { return filepath.Dir(c.path) }

// CSVAbs devuelve la ruta absoluta del CSV. Una ruta relativa se toma respecto a la
// carpeta de config.json, no del directorio actual (el Programador de tareas arranca en System32).
func (c *Config) CSVAbs() string {
	p := c.CSVPath
	if p == "" {
		p = "contadores.csv"
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir(), p)
}

func (c *Config) normalizar() {
	def := Default()
	if c.Community == "" {
		c.Community = def.Community
	}
	if c.SNMPVersion == "" {
		c.SNMPVersion = def.SNMPVersion
	}
	if c.TimeoutMs <= 0 {
		c.TimeoutMs = def.TimeoutMs
	}
	if c.Retries < 0 {
		c.Retries = 0
	}
	if c.CSVPath == "" {
		c.CSVPath = def.CSVPath
	}
	if c.RedTimeoutMs <= 0 {
		c.RedTimeoutMs = def.RedTimeoutMs
	}
	if c.RedConcurrencia <= 0 {
		c.RedConcurrencia = def.RedConcurrencia
	}
	if c.Printers == nil {
		c.Printers = []Printer{}
	}
	for i := range c.Printers {
		p := &c.Printers[i]
		p.IP = strings.TrimSpace(p.IP)
		p.Nombre = strings.TrimSpace(p.Nombre)
		p.Marca = strings.ToLower(strings.TrimSpace(p.Marca))
		if p.Marca == "" {
			p.Marca = counters.MarcaAuto
		}
		p.Origen = strings.ToLower(strings.TrimSpace(p.Origen))
		if p.Origen == "" {
			p.Origen = OrigenManual
		}
	}
}

// Validate revisa los valores. No toca la red.
func (c *Config) Validate() error {
	if _, err := snmp.ParseVersion(c.SNMPVersion); err != nil {
		return err
	}
	vistas := map[string]bool{}
	for i, p := range c.Printers {
		if err := ValidarIP(p.IP); err != nil {
			return fmt.Errorf("impresora %d (%s): %w", i+1, p.Etiqueta(), err)
		}
		switch p.Origen {
		case OrigenInstalada, OrigenManual, OrigenRed:
		default:
			return fmt.Errorf("impresora %d (%s): origen %q inválido (instalada, manual o red)", i+1, p.Etiqueta(), p.Origen)
		}
		if vistas[p.IP] {
			return fmt.Errorf("la IP %s está repetida en printers", p.IP)
		}
		vistas[p.IP] = true
	}
	return nil
}

// ValidarIP acepta sólo IPv4 en formato punto-decimal.
func ValidarIP(s string) error {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || ip.To4() == nil || strings.Contains(s, ":") {
		return fmt.Errorf("%q no es una IPv4 válida", s)
	}
	return nil
}

// Save escribe de forma atómica: archivo temporal en la misma carpeta y rename.
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("guardar configuración: no hay ruta asignada")
	}
	c.normalizar()
	if err := c.Validate(); err != nil {
		return fmt.Errorf("guardar configuración: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar configuración: %w", err)
	}
	data = append(data, '\r', '\n')
	return WriteAtomic(c.path, data)
}

// WriteAtomic escribe data en path vía temporal + rename (en Windows, os.Rename
// reemplaza el destino con MoveFileEx).
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("crear temporal en %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	limpiar := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		limpiar()
		return fmt.Errorf("escribir %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		limpiar()
		return fmt.Errorf("sincronizar %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		limpiar()
		return fmt.Errorf("cerrar %s: %w", tmpName, err)
	}
	// Reintento corto: un antivirus o el indexador pueden tener el destino abierto un instante.
	for i := 0; ; i++ {
		err = os.Rename(tmpName, path)
		if err == nil || i == 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		limpiar()
		return fmt.Errorf("reemplazar %s: %w", path, err)
	}
	return nil
}

// Params arma los parámetros SNMP para una IP con los valores de la configuración.
func (c *Config) Params(ip string) snmp.Params {
	return snmp.Params{
		Target:    ip,
		Community: c.Community,
		Version:   c.SNMPVersion,
		Timeout:   time.Duration(c.TimeoutMs) * time.Millisecond,
		Retries:   c.Retries,
	}
}

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
