// Package brother es el driver de contadores para impresoras Brother (BROTHER-MIB).
package brother

import (
	"fmt"
	"sort"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// OIDPrintPages es la rama de contadores de Brother. Cada tabla N vive en
// OIDPrintPages.N.1.<columna>.<índice>; columna 2 = código y columna 3 = páginas.
const OIDPrintPages = "1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.52"

// Tablas que usamos.
const (
	TablaTamanos   = 1  // páginas por tamaño de papel
	TablaFunciones = 21 // páginas por función (PC, copia, fax; total/color/mono)
)

// Fila es una fila de una tabla de contadores Brother.
type Fila struct {
	Indice  int
	Codigo  int
	Paginas int64
}

// ParseTable extrae la tabla n de un walk de OIDPrintPages, ordenada por índice.
// Las filas a las que les falta el código o las páginas se omiten.
func ParseTable(walk []snmp.Var, n int) ([]Fila, error) {
	root := fmt.Sprintf("%s.%d.1", OIDPrintPages, n)
	type parcial struct {
		cod, pag       int64
		hayCod, hayPag bool
	}
	rows := map[int]*parcial{}
	for _, v := range walk {
		if !snmp.HasPrefix(v.OID, root) || v.OID == root {
			continue
		}
		sub, err := snmp.SubOID(v.OID, root)
		if err != nil {
			return nil, fmt.Errorf("tabla %d: %w", n, err)
		}
		if len(sub) != 2 {
			continue // no es columna.índice
		}
		col, idx := sub[0], sub[1]
		num, ok := v.Int64()
		if !ok {
			continue
		}
		p := rows[idx]
		if p == nil {
			p = &parcial{}
			rows[idx] = p
		}
		switch col {
		case 2:
			p.cod, p.hayCod = num, true
		case 3:
			p.pag, p.hayPag = num, true
		}
	}
	out := make([]Fila, 0, len(rows))
	for idx, p := range rows {
		if p.hayCod && p.hayPag {
			out = append(out, Fila{Indice: idx, Codigo: int(p.cod), Paginas: p.pag})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Indice < out[j].Indice })
	return out, nil
}
