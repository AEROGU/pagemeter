package snmp

import (
	"context"
	"fmt"
)

// Mock es un Client en memoria. Sirve para tests y para reproducir walks guardados.
type Mock struct {
	Vars []Var // no hace falta que vengan ordenadas
	// Err, si no es nil, lo devuelven todas las llamadas (simula un equipo que no responde).
	Err error
	// FailWalk hace fallar sólo los Walk bajo esas raíces.
	FailWalk map[string]error
}

// NewMock crea un mock con esas variables.
func NewMock(vars ...Var) *Mock {
	m := &Mock{Vars: append([]Var(nil), vars...)}
	SortVars(m.Vars)
	return m
}

// MockDialer devuelve un Dialer que entrega el mock según la IP destino.
// Si la IP no está en el mapa, simula falta de respuesta.
func MockDialer(byTarget map[string]*Mock) Dialer {
	return func(p Params) (Client, error) {
		if m, ok := byTarget[p.Target]; ok {
			return m, nil
		}
		return &Mock{Err: fmt.Errorf("%s: %w", p.Target, ErrNoResponse)}, nil
	}
}

func (m *Mock) Close() error { return nil }

func (m *Mock) Get(ctx context.Context, oids ...string) ([]Var, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.Err != nil {
		return nil, m.Err
	}
	out := make([]Var, 0, len(oids))
	for _, o := range oids {
		o = NormOID(o)
		found := Var{OID: o, Kind: KindNull}
		for _, v := range m.Vars {
			if v.OID == o {
				found = v
				break
			}
		}
		out = append(out, found)
	}
	return out, nil
}

func (m *Mock) Walk(ctx context.Context, root string) ([]Var, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.Err != nil {
		return nil, m.Err
	}
	for r, err := range m.FailWalk {
		if HasPrefix(root, r) || HasPrefix(r, root) {
			return nil, err
		}
	}
	vs := append([]Var(nil), m.Vars...)
	SortVars(vs)
	var out []Var
	for _, v := range vs {
		if HasPrefix(v.OID, root) && v.Kind != KindNull {
			out = append(out, v)
		}
	}
	return out, nil
}
