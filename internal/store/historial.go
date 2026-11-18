package store

import (
	"sort"
	"strings"
	"time"
)

// Escaneo agrupa las filas que comparten escaneo_id.
type Escaneo struct {
	ID    string
	Fecha time.Time
	Filas []Fila
}

// OK y Errores cuentan filas por estado.
func (e Escaneo) OK() int {
	n := 0
	for _, f := range e.Filas {
		if f.Estado == EstadoOK {
			n++
		}
	}
	return n
}

func (e Escaneo) Errores() int { return len(e.Filas) - e.OK() }

// Agrupar arma los escaneos, del más reciente al más antiguo.
func Agrupar(filas []Fila) []Escaneo {
	pos := map[string]int{}
	var out []Escaneo
	for _, f := range filas {
		id := f.EscaneoID
		if id == "" {
			id = NuevoEscaneoID(f.Fecha) // CSV editado a mano sin id
		}
		i, ok := pos[id]
		if !ok {
			i = len(out)
			pos[id] = i
			out = append(out, Escaneo{ID: id, Fecha: f.Fecha})
		}
		if f.Fecha.Before(out[i].Fecha) {
			out[i].Fecha = f.Fecha
		}
		out[i].Filas = append(out[i].Filas, f)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Fecha.After(out[b].Fecha) })
	return out
}

// FiltrarImpresora deja sólo las filas cuyo nombre o IP contengan el texto
// (sin distinguir mayúsculas). Los escaneos que quedan vacíos se descartan.
func FiltrarImpresora(es []Escaneo, texto string) []Escaneo {
	texto = strings.ToLower(strings.TrimSpace(texto))
	if texto == "" {
		return es
	}
	var out []Escaneo
	for _, e := range es {
		var fs []Fila
		for _, f := range e.Filas {
			if strings.Contains(strings.ToLower(f.Impresora), texto) || strings.Contains(f.IP, texto) {
				fs = append(fs, f)
			}
		}
		if len(fs) > 0 {
			e.Filas = fs
			out = append(out, e)
		}
	}
	return out
}

// UltimaPorImpresora devuelve la fila más reciente de cada impresora (por IP).
func UltimaPorImpresora(filas []Fila) map[string]Fila {
	m := map[string]Fila{}
	for _, f := range filas {
		if prev, ok := m[f.Clave()]; !ok || !f.Fecha.Before(prev.Fecha) {
			m[f.Clave()] = f
		}
	}
	return m
}

// DifFila es el consumo de una impresora entre dos escaneos.
type DifFila struct {
	Clave     string
	Impresora string
	IP        string
	// Delta por columna (mismo orden que ColumnasNum). nil si falta el valor en
	// alguno de los dos escaneos, o si la impresora no estaba / falló en uno de ellos.
	Delta [numCols]*int64
	// Nota explica por qué faltan datos ("sólo en el escaneo final", "error en ...").
	Nota string
}

// Diferencia calcula despues - antes por impresora y columna. Si se pasan en
// orden inverso, los ordena por fecha. Devuelve también el escaneo antes/después usado.
func Diferencia(a, b Escaneo) (antes, despues Escaneo, filas []DifFila) {
	antes, despues = a, b
	if despues.Fecha.Before(antes.Fecha) {
		antes, despues = despues, antes
	}
	ma := map[string]Fila{}
	for _, f := range antes.Filas {
		ma[f.Clave()] = f
	}
	vistos := map[string]bool{}
	for _, fd := range despues.Filas {
		k := fd.Clave()
		vistos[k] = true
		d := DifFila{Clave: k, Impresora: fd.Impresora, IP: fd.IP}
		fa, ok := ma[k]
		switch {
		case !ok:
			d.Nota = "sólo en el escaneo final"
		case fa.Estado != EstadoOK:
			d.Nota = "error en el escaneo inicial"
		case fd.Estado != EstadoOK:
			d.Nota = "error en el escaneo final"
		default:
			for i := range d.Delta {
				if fa.Num[i] != nil && fd.Num[i] != nil {
					v := *fd.Num[i] - *fa.Num[i]
					d.Delta[i] = &v
				}
			}
			if d.Delta[ColTotal] != nil && *d.Delta[ColTotal] < 0 {
				d.Nota = "el contador bajó (¿reemplazo de tarjeta o de impresora?)"
			}
		}
		filas = append(filas, d)
	}
	for _, fa := range antes.Filas {
		if !vistos[fa.Clave()] {
			filas = append(filas, DifFila{Clave: fa.Clave(), Impresora: fa.Impresora, IP: fa.IP, Nota: "sólo en el escaneo inicial"})
		}
	}
	return antes, despues, filas
}
