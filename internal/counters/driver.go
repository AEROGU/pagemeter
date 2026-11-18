package counters

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// MarcaAuto en la configuración pide detectar el driver por sysDescr.
const MarcaAuto = "auto"

// Driver lee los contadores propios de una marca. La parte estándar
// (sysDescr y total de Printer-MIB) ya viene llena en r cuando se llama a Lee.
type Driver interface {
	// Marca es el identificador en minúsculas ("brother"). Va al CSV y a config.json.
	Marca() string
	// Detecta indica si el sysDescr corresponde a esta marca.
	Detecta(sysDescr string) bool
	// Lee completa r con los datos de la marca. Un error aquí no anula el total.
	Lee(ctx context.Context, c snmp.Client, r *Reading) error
}

// Registro guarda los drivers disponibles. El genérico siempre está al final.
type Registro struct {
	drivers []Driver
}

// NuevoRegistro crea un registro con esos drivers, más el genérico como respaldo.
func NuevoRegistro(ds ...Driver) *Registro {
	return &Registro{drivers: append(append([]Driver(nil), ds...), Generico{})}
}

// Drivers devuelve los drivers registrados (incluido el genérico).
func (r *Registro) Drivers() []Driver { return append([]Driver(nil), r.drivers...) }

// Buscar devuelve el driver con esa marca.
func (r *Registro) Buscar(marca string) (Driver, bool) {
	marca = strings.ToLower(strings.TrimSpace(marca))
	for _, d := range r.drivers {
		if d.Marca() == marca {
			return d, true
		}
	}
	return nil, false
}

// Elegir decide el driver: el indicado por marca o, con "auto"/"", el primero
// que reconozca el sysDescr; si ninguno, el genérico.
func (r *Registro) Elegir(marca, sysDescr string) (Driver, error) {
	m := strings.ToLower(strings.TrimSpace(marca))
	if m != "" && m != MarcaAuto {
		d, ok := r.Buscar(m)
		if !ok {
			return nil, fmt.Errorf("no hay driver para la marca %q", marca)
		}
		return d, nil
	}
	for _, d := range r.drivers {
		if d.Detecta(sysDescr) {
			return d, nil
		}
	}
	return Generico{}, nil
}

// Reconoce indica si algún driver de marca (no el genérico) reconoce el sysDescr.
func (r *Registro) Reconoce(sysDescr string) bool {
	for _, d := range r.drivers {
		if _, gen := d.(Generico); !gen && d.Detecta(sysDescr) {
			return true
		}
	}
	return false
}

// Leer hace la lectura completa de una impresora:
//  1. sysDescr (si falla, la impresora se considera sin respuesta y devuelve error);
//  2. total por Printer-MIB (si falla, queda como aviso);
//  3. datos de la marca con el driver elegido (si falla, queda como aviso).
func (r *Registro) Leer(ctx context.Context, c snmp.Client, marca string) (Reading, error) {
	var rd Reading
	sd, err := snmp.SysDescr(ctx, c)
	if err != nil {
		return rd, err
	}
	rd.SysDescr = sd

	drv, err := r.Elegir(marca, sd)
	if err != nil {
		return rd, err
	}
	rd.Marca = drv.Marca()

	total, vs, err := snmp.LifeCount(ctx, c)
	if err != nil {
		if ctxErr(err) {
			return rd, err
		}
		rd.Avisos = append(rd.Avisos, err.Error())
	}
	rd.Total = total
	rd.Crudos = append(rd.Crudos, snmp.StrVar(snmp.OIDSysDescr, sd))
	rd.Crudos = append(rd.Crudos, vs...)

	if err := drv.Lee(ctx, c, &rd); err != nil {
		if ctxErr(err) {
			return rd, err
		}
		rd.Avisos = append(rd.Avisos, fmt.Sprintf("%s: %v", drv.Marca(), err))
	}
	return rd, nil
}

func ctxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Generico no lee nada propio de una marca: sólo queda el total de Printer-MIB.
type Generico struct{}

func (Generico) Marca() string                                    { return "generico" }
func (Generico) Detecta(string) bool                              { return false }
func (Generico) Lee(context.Context, snmp.Client, *Reading) error { return nil }
