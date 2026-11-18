package config

import (
	"fmt"
	"strings"

	"github.com/AEROGU/pagemeter/internal/counters"
)

// Clone devuelve una copia independiente (para pasarla a goroutines sin carreras).
func (c *Config) Clone() *Config {
	cp := *c
	cp.Printers = append([]Printer(nil), c.Printers...)
	return &cp
}

// Buscar devuelve el índice de la impresora con esa IP, o -1.
func (c *Config) Buscar(ip string) int {
	ip = strings.TrimSpace(ip)
	for i, p := range c.Printers {
		if p.IP == ip {
			return i
		}
	}
	return -1
}

// Agregar añade una impresora. Falla si la IP ya existe.
func (c *Config) Agregar(p Printer) error {
	p.IP = strings.TrimSpace(p.IP)
	if err := ValidarIP(p.IP); err != nil {
		return err
	}
	if i := c.Buscar(p.IP); i >= 0 {
		return fmt.Errorf("la IP %s ya está registrada como %q", p.IP, c.Printers[i].Etiqueta())
	}
	if p.Marca == "" {
		p.Marca = counters.MarcaAuto
	}
	c.Printers = append(c.Printers, p)
	return nil
}

// Editar reemplaza la impresora con IP vieja por p (puede cambiar la IP).
func (c *Config) Editar(ipVieja string, p Printer) error {
	i := c.Buscar(ipVieja)
	if i < 0 {
		return fmt.Errorf("no existe la impresora %s", ipVieja)
	}
	p.IP = strings.TrimSpace(p.IP)
	if err := ValidarIP(p.IP); err != nil {
		return err
	}
	if j := c.Buscar(p.IP); j >= 0 && j != i {
		return fmt.Errorf("la IP %s ya está registrada como %q", p.IP, c.Printers[j].Etiqueta())
	}
	if p.Marca == "" {
		p.Marca = counters.MarcaAuto
	}
	c.Printers[i] = p
	return nil
}

// Eliminar quita la impresora con esa IP. Devuelve false si no existía.
func (c *Config) Eliminar(ip string) bool {
	i := c.Buscar(ip)
	if i < 0 {
		return false
	}
	c.Printers = append(c.Printers[:i], c.Printers[i+1:]...)
	return true
}

// SetHabilitada marca o desmarca una impresora.
func (c *Config) SetHabilitada(ip string, v bool) error {
	i := c.Buscar(ip)
	if i < 0 {
		return fmt.Errorf("no existe la impresora %s", ip)
	}
	c.Printers[i].Habilitada = v
	return nil
}

// Habilitadas devuelve las impresoras marcadas.
func (c *Config) Habilitadas() []Printer {
	var out []Printer
	for _, p := range c.Printers {
		if p.Habilitada {
			out = append(out, p)
		}
	}
	return out
}

// FusionarInstaladas agrega las impresoras instaladas cuya IP no esté ya en la
// configuración (habilitadas por defecto). Las que ya existen no se tocan, para
// respetar nombre, marca y casilla que haya elegido el usuario. Devuelve cuántas agregó.
func (c *Config) FusionarInstaladas(ps []Printer) int {
	n := 0
	for _, p := range ps {
		p.Origen = OrigenInstalada
		p.Habilitada = true
		if c.Agregar(p) == nil {
			n++
		}
	}
	return n
}
