package brother

import (
	"context"
	"fmt"
	"strings"

	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
)

// Driver implementa counters.Driver para Brother.
type Driver struct{}

var _ counters.Driver = Driver{}

func (Driver) Marca() string { return "brother" }

func (Driver) Detecta(sysDescr string) bool {
	return strings.Contains(strings.ToLower(sysDescr), "brother")
}

// Lee recorre la rama printPages y llena funciones, tamaños y desgloses.
func (Driver) Lee(ctx context.Context, c snmp.Client, r *counters.Reading) error {
	walk, err := c.Walk(ctx, OIDPrintPages)
	if err != nil {
		return fmt.Errorf("leer tablas de contadores Brother: %w", err)
	}
	r.Crudos = append(r.Crudos, walk...)
	if len(walk) == 0 {
		return fmt.Errorf("la impresora no reporta la rama de contadores Brother (%s)", OIDPrintPages)
	}
	return Decodificar(walk, r)
}

// Decodificar interpreta un walk de OIDPrintPages y llena r. Está separado de Lee
// para poder probarlo directamente con el fixture.
func Decodificar(walk []snmp.Var, r *counters.Reading) error {
	fun, err := ParseTable(walk, TablaFunciones)
	if err != nil {
		return err
	}
	tam, err := ParseTable(walk, TablaTamanos)
	if err != nil {
		return err
	}

	r.Funciones = r.Funciones[:0]
	codigos := map[int]int64{}
	for _, f := range fun {
		n, ok := NombreFuncion(f.Codigo)
		r.Funciones = append(r.Funciones, counters.Linea{Codigo: f.Codigo, Nombre: n, Conocido: ok, Paginas: f.Paginas})
		if _, dup := codigos[f.Codigo]; !dup {
			codigos[f.Codigo] = f.Paginas
		}
	}
	r.Tamanos = r.Tamanos[:0]
	for _, t := range tam {
		n, ok := NombreTamano(t.Codigo)
		r.Tamanos = append(r.Tamanos, counters.Linea{Codigo: t.Codigo, Nombre: n, Conocido: ok, Paginas: t.Paginas})
	}

	get := func(cod int) *int64 {
		if v, ok := codigos[cod]; ok {
			return counters.Ptr(v)
		}
		return nil
	}
	r.PC = counters.Desglose{Total: get(FuncPCTotal), Color: get(FuncPCColor), Mono: get(FuncPCMono)}
	r.Copia = counters.Desglose{Total: get(FuncCopiaTotal), Color: get(FuncCopiaColor), Mono: get(FuncCopiaMono)}
	r.Fax = counters.Desglose{Total: get(FuncFaxTotal), Color: get(FuncFaxColor), Mono: get(FuncFaxMono)}
	return nil
}
