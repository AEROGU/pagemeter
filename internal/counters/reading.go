// Package counters define el modelo de contadores, neutro respecto a la marca,
// y el mecanismo de drivers por marca.
package counters

import (
	"strconv"
	"strings"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// Desglose es total/color/mono de una función. nil = la impresora no lo reporta.
type Desglose struct {
	Total, Color, Mono *int64
}

// Linea es una fila de un desglose crudo (por función o por tamaño).
type Linea struct {
	Codigo   int
	Nombre   string // nombre decodificado, o "Código N (no documentado)"
	Conocido bool
	Paginas  int64
}

// Reading es el resultado de leer una impresora.
type Reading struct {
	Marca    string // driver que la leyó: "brother", "generico"...
	SysDescr string
	Total    *int64 // prtMarkerLifeCount

	PC, Copia, Fax Desglose

	Funciones []Linea // desglose por función, en el orden que lo reporta el equipo
	Tamanos   []Linea // desglose por tamaño de papel

	// Avisos: problemas no fatales (p. ej. falló la tabla de la marca pero hubo total).
	Avisos []string
	// Crudos: todas las variables leídas, para la vista de OIDs.
	Crudos []snmp.Var
}

// FormatRaw convierte un desglose al formato del CSV: "1=64002;3=64850;91=101".
func FormatRaw(ls []Linea) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = strconv.Itoa(l.Codigo) + "=" + strconv.FormatInt(l.Paginas, 10)
	}
	return strings.Join(parts, ";")
}

// Ptr es un atajo para construir *int64.
func Ptr(n int64) *int64 { return &n }
