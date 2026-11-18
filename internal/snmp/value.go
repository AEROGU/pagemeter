// Package snmp es el transporte SNMP genérico: no sabe nada de marcas de impresoras.
package snmp

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind es el tipo de un valor SNMP, ya simplificado.
type Kind int

const (
	KindNull   Kind = iota // Null, noSuchObject, noSuchInstance, endOfMibView
	KindInt                // INTEGER (con signo)
	KindUint               // Counter32, Gauge32, TimeTicks, Counter64, Uinteger32
	KindString             // OCTET STRING
	KindOID                // OBJECT IDENTIFIER
	KindOther              // cualquier otro (IpAddress, Opaque...), guardado como texto
)

// Var es un par OID/valor. El OID va sin punto inicial (p. ej. "1.3.6.1.2.1.1.1.0").
type Var struct {
	OID  string
	Kind Kind
	Int  int64  // válido si Kind == KindInt
	Uint uint64 // válido si Kind == KindUint
	Str  string // válido si Kind es KindString, KindOID o KindOther
}

// IntVar y StrVar son atajos para construir valores (útiles en tests y mocks).
func IntVar(oid string, v int64) Var   { return Var{OID: NormOID(oid), Kind: KindInt, Int: v} }
func StrVar(oid string, s string) Var  { return Var{OID: NormOID(oid), Kind: KindString, Str: s} }
func UintVar(oid string, v uint64) Var { return Var{OID: NormOID(oid), Kind: KindUint, Uint: v} }

// Int64 devuelve el valor numérico, si lo es y cabe en int64.
func (v Var) Int64() (int64, bool) {
	switch v.Kind {
	case KindInt:
		return v.Int, true
	case KindUint:
		if v.Uint > math.MaxInt64 {
			return 0, false
		}
		return int64(v.Uint), true
	case KindString:
		// Algunos equipos devuelven números como texto.
		n, err := strconv.ParseInt(strings.TrimSpace(v.Str), 10, 64)
		return n, err == nil
	}
	return 0, false
}

// String devuelve el valor en texto, como lo mostraría la vista de OIDs crudos.
func (v Var) String() string {
	switch v.Kind {
	case KindInt:
		return strconv.FormatInt(v.Int, 10)
	case KindUint:
		return strconv.FormatUint(v.Uint, 10)
	case KindNull:
		return ""
	}
	return v.Str
}

// NormOID quita espacios y el punto inicial.
func NormOID(oid string) string {
	return strings.TrimPrefix(strings.TrimSpace(oid), ".")
}

// HasPrefix indica si oid está bajo la rama root (o es igual a ella).
func HasPrefix(oid, root string) bool {
	oid, root = NormOID(oid), NormOID(root)
	return oid == root || strings.HasPrefix(oid, root+".")
}

// CompareOID compara dos OIDs numéricamente, componente por componente.
func CompareOID(a, b string) int {
	pa := strings.Split(NormOID(a), ".")
	pb := strings.Split(NormOID(b), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.ParseUint(pa[i], 10, 64)
		nb, eb := strconv.ParseUint(pb[i], 10, 64)
		if ea != nil || eb != nil {
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
			continue
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}

// SortVars ordena por OID numéricamente.
func SortVars(vs []Var) {
	sort.SliceStable(vs, func(i, j int) bool { return CompareOID(vs[i].OID, vs[j].OID) < 0 })
}

// SubOID devuelve los componentes de oid que siguen a root, como enteros.
// Ejemplo: SubOID("1.2.3.4.5", "1.2.3") = [4 5].
func SubOID(oid, root string) ([]int, error) {
	oid, root = NormOID(oid), NormOID(root)
	if !strings.HasPrefix(oid, root+".") {
		return nil, fmt.Errorf("el OID %s no está bajo %s", oid, root)
	}
	parts := strings.Split(oid[len(root)+1:], ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("componente %q inválido en %s: %w", p, oid, err)
		}
		out[i] = n
	}
	return out, nil
}
