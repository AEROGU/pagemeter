// Package store maneja el CSV acumulativo de lecturas: sólo agrega filas, nunca reescribe.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/AEROGU/pagemeter/internal/counters"
)

// FormatoFecha es el formato de la columna fecha (hora local).
const FormatoFecha = "2006-01-02 15:04:05"

// Estados de una fila.
const (
	EstadoOK    = "ok"
	EstadoError = "error"
)

// Columnas numéricas, en el orden del CSV. El índice es el de Fila.Num.
var ColumnasNum = []string{
	"total",
	"pc_total", "pc_color", "pc_mono",
	"copia_total", "copia_color", "copia_mono",
	"fax_total", "fax_color", "fax_mono",
}

// Índices en Fila.Num.
const (
	ColTotal = iota
	ColPCTotal
	ColPCColor
	ColPCMono
	ColCopiaTotal
	ColCopiaColor
	ColCopiaMono
	ColFaxTotal
	ColFaxColor
	ColFaxMono
	numCols
)

// Encabezado completo del CSV.
var Encabezado = func() []string {
	h := []string{"fecha", "escaneo_id", "impresora", "marca", "ip", "sysdescr", "estado", "error"}
	h = append(h, ColumnasNum...)
	return append(h, "funciones_raw", "tamanos_raw")
}()

// Fila es una fila del CSV: una impresora en un escaneo.
type Fila struct {
	Fecha        time.Time
	EscaneoID    string
	Impresora    string
	Marca        string
	IP           string
	SysDescr     string
	Estado       string
	Error        string
	Num          [numCols]*int64 // nil = celda vacía (la impresora no lo reporta)
	FuncionesRaw string
	TamanosRaw   string
}

// Valor devuelve la columna numérica por nombre ("pc_total"...).
func (f Fila) Valor(col string) *int64 {
	for i, c := range ColumnasNum {
		if c == col {
			return f.Num[i]
		}
	}
	return nil
}

// NuevoEscaneoID genera el identificador de un escaneo a partir de su fecha.
func NuevoEscaneoID(t time.Time) string { return t.Format("20060102-150405") }

// DesdeLectura arma la fila de una impresora. Si errLectura no es nil, la fila
// queda con estado=error (y lo que se haya alcanzado a leer).
func DesdeLectura(fecha time.Time, escaneoID, impresora, ip string, r counters.Reading, errLectura error) Fila {
	f := Fila{
		Fecha:     fecha,
		EscaneoID: escaneoID,
		Impresora: impresora,
		Marca:     r.Marca,
		IP:        ip,
		SysDescr:  r.SysDescr,
		Estado:    EstadoOK,
	}
	var msgs []string
	if errLectura != nil {
		f.Estado = EstadoError
		msgs = append(msgs, errLectura.Error())
	}
	// Los avisos (fallos parciales) van en la columna error aunque el estado sea ok.
	msgs = append(msgs, r.Avisos...)
	f.Error = strings.Join(msgs, " | ")

	f.Num[ColTotal] = r.Total
	f.Num[ColPCTotal], f.Num[ColPCColor], f.Num[ColPCMono] = r.PC.Total, r.PC.Color, r.PC.Mono
	f.Num[ColCopiaTotal], f.Num[ColCopiaColor], f.Num[ColCopiaMono] = r.Copia.Total, r.Copia.Color, r.Copia.Mono
	f.Num[ColFaxTotal], f.Num[ColFaxColor], f.Num[ColFaxMono] = r.Fax.Total, r.Fax.Color, r.Fax.Mono
	f.FuncionesRaw = counters.FormatRaw(r.Funciones)
	f.TamanosRaw = counters.FormatRaw(r.Tamanos)
	return f
}

// Clave identifica a la impresora entre escaneos: la IP, o el nombre si no hay IP.
func (f Fila) Clave() string {
	if f.IP != "" {
		return f.IP
	}
	return "nombre:" + f.Impresora
}

func (f Fila) String() string {
	return fmt.Sprintf("%s %s %s (%s) %s", f.Fecha.Format(FormatoFecha), f.EscaneoID, f.Impresora, f.IP, f.Estado)
}
