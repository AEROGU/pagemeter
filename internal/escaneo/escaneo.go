// Package escaneo lee las impresoras habilitadas en paralelo y agrega el
// resultado al CSV. Lo usan la TUI y el modo --scan.
package escaneo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/store"
)

// Estados de progreso por impresora.
const (
	EstadoPendiente = "pendiente"
	EstadoLeyendo   = "leyendo"
	EstadoOK        = "ok"
	EstadoError     = "error"
)

// Concurrencia es cuántas impresoras se leen a la vez.
const Concurrencia = 8

// TimeoutImpresora limita el tiempo total de lectura de una impresora.
const TimeoutImpresora = 90 * time.Second

// Lectura es el resultado de una impresora.
type Lectura struct {
	Printer config.Printer
	Reading counters.Reading
	Err     error
	Fila    store.Fila
}

// Resultado es un escaneo completo.
type Resultado struct {
	ID       string
	Fecha    time.Time
	Lecturas []Lectura
	CSVPath  string
	CSVErr   error // error al escribir el CSV (las lecturas siguen siendo válidas)
}

// Fallidas cuenta las impresoras con error.
func (r Resultado) Fallidas() int {
	n := 0
	for _, l := range r.Lecturas {
		if l.Err != nil {
			n++
		}
	}
	return n
}

// Progreso se llama cada vez que cambia el estado de una impresora (índice en
// cfg.Habilitadas()). Puede llamarse desde varias goroutines a la vez.
type Progreso func(i int, estado string, err error)

// Ejecutar lee las impresoras habilitadas de cfg y agrega las filas al CSV.
// cfg debe ser una copia (Clone) si la UI puede modificarla mientras tanto.
func Ejecutar(ctx context.Context, cfg *config.Config, reg *counters.Registro, dial snmp.Dialer, prog Progreso) Resultado {
	if dial == nil {
		dial = snmp.Dial
	}
	if prog == nil {
		prog = func(int, string, error) {}
	}
	ps := cfg.Habilitadas()
	fecha := time.Now().Truncate(time.Second)
	res := Resultado{ID: store.NuevoEscaneoID(fecha), Fecha: fecha, CSVPath: cfg.CSVAbs()}
	res.Lecturas = make([]Lectura, len(ps))

	sem := make(chan struct{}, Concurrencia)
	var wg sync.WaitGroup
	for i, p := range ps {
		res.Lecturas[i].Printer = p
		prog(i, EstadoPendiente, nil)
		wg.Add(1)
		go func(i int, p config.Printer) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				res.Lecturas[i].Err = ctx.Err()
				prog(i, EstadoError, ctx.Err())
				return
			}
			defer func() { <-sem }()
			prog(i, EstadoLeyendo, nil)
			r, err := Leer(ctx, cfg, reg, dial, p.IP, p.Marca)
			res.Lecturas[i].Reading, res.Lecturas[i].Err = r, err
			if err != nil {
				prog(i, EstadoError, err)
			} else {
				prog(i, EstadoOK, nil)
			}
		}(i, p)
	}
	wg.Wait()

	filas := make([]store.Fila, len(res.Lecturas))
	for i := range res.Lecturas {
		l := &res.Lecturas[i]
		l.Fila = store.DesdeLectura(fecha, res.ID, l.Printer.Etiqueta(), l.Printer.IP, l.Reading, l.Err)
		filas[i] = l.Fila
	}
	if ctx.Err() != nil {
		res.CSVErr = fmt.Errorf("escaneo cancelado; no se escribió el CSV: %w", ctx.Err())
		return res
	}
	if err := store.Append(res.CSVPath, filas); err != nil {
		res.CSVErr = fmt.Errorf("escribir CSV: %w", err)
	}
	return res
}

// Leer abre un cliente y hace la lectura completa de una impresora.
func Leer(ctx context.Context, cfg *config.Config, reg *counters.Registro, dial snmp.Dialer, ip, marca string) (counters.Reading, error) {
	if dial == nil {
		dial = snmp.Dial
	}
	ctx, cancel := context.WithTimeout(ctx, TimeoutImpresora)
	defer cancel()
	c, err := dial(cfg.Params(ip))
	if err != nil {
		return counters.Reading{}, err
	}
	defer c.Close()
	r, err := reg.Leer(ctx, c, marca)
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("%s: la lectura tardó más de %v", ip, TimeoutImpresora)
	}
	return r, err
}

// Prueba es el resultado de probar una IP (botón "Probar" y tecla P).
type Prueba struct {
	Reading counters.Reading
	Modelo  string // de hrDeviceDescr, si lo reporta
}

// Probar lee una impresora y además intenta obtener su modelo.
func Probar(ctx context.Context, cfg *config.Config, reg *counters.Registro, dial snmp.Dialer, ip, marca string) (Prueba, error) {
	if dial == nil {
		dial = snmp.Dial
	}
	ctx, cancel := context.WithTimeout(ctx, TimeoutImpresora)
	defer cancel()
	c, err := dial(cfg.Params(ip))
	if err != nil {
		return Prueba{}, err
	}
	defer c.Close()
	r, err := reg.Leer(ctx, c, marca)
	if err != nil {
		return Prueba{Reading: r}, err
	}
	return Prueba{Reading: r, Modelo: snmp.DeviceModel(ctx, c)}, nil
}
