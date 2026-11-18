// pagemeter lee por SNMP los contadores de impresión de impresoras de red
// (por ahora Brother) y los acumula en un CSV.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/AEROGU/pagemeter/internal/config"
	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/escaneo"
	"github.com/AEROGU/pagemeter/internal/snmp"
	"github.com/AEROGU/pagemeter/internal/tui"
	"github.com/AEROGU/pagemeter/internal/vendors/brother"
)

// Códigos de salida.
const (
	salidaOK      = 0
	salidaFallas  = 1 // al menos una impresora no se pudo leer
	salidaGeneral = 2 // configuración, CSV, argumentos o nada que escanear
)

// registro devuelve los drivers disponibles. Una marca nueva se agrega aquí.
func registro() *counters.Registro {
	return counters.NuevoRegistro(brother.Driver{})
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil))
}

// run es el programa completo; dial == nil usa SNMP real.
func run(args []string, out, errOut io.Writer, dial snmp.Dialer) int {
	fs := flag.NewFlagSet("pagemeter", flag.ContinueOnError)
	fs.SetOutput(errOut)
	rutaConfig := fs.String("config", "", "ruta de config.json (por defecto, junto al .exe)")
	scan := fs.Bool("scan", false, "escanea las impresoras habilitadas, agrega al CSV y sale")
	fs.Usage = func() {
		fmt.Fprintln(errOut, "Uso: pagemeter [--config <ruta>] [--scan]")
		fmt.Fprintln(errOut, "Sin --scan abre la interfaz de consola.")
		fs.PrintDefaults()
		fmt.Fprintln(errOut, "Con --scan sale con 0 si todo fue bien, 1 si falló alguna impresora y 2 ante otros errores.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return salidaOK
		}
		return salidaGeneral
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errOut, "Error: argumento no reconocido: %s\n", fs.Arg(0))
		fs.Usage()
		return salidaGeneral
	}

	cfg, err := cargarConfig(*rutaConfig)
	if err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return salidaGeneral
	}
	if *scan {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return escanear(ctx, cfg, registro(), dial, out, errOut)
	}
	ui := tui.New(cfg, registro(), dial)
	if err := ui.UsarConsolaWindows(); err != nil {
		fmt.Fprintln(errOut, "Error:", err)
		return salidaGeneral
	}
	if err := ui.Run(); err != nil {
		fmt.Fprintln(errOut, "Error en la interfaz:", err)
		return salidaGeneral
	}
	return salidaOK
}

// escanear es el modo no interactivo (--scan).
func escanear(ctx context.Context, cfg *config.Config, reg *counters.Registro, dial snmp.Dialer, out, errOut io.Writer) int {
	if len(cfg.Habilitadas()) == 0 {
		fmt.Fprintf(errOut, "Error: no hay impresoras habilitadas en %s\n", cfg.Path())
		return salidaGeneral
	}
	res := escaneo.Ejecutar(ctx, cfg, reg, dial, nil)
	fmt.Fprintf(out, "Escaneo %s: %d ok, %d con error\n", res.ID, len(res.Lecturas)-res.Fallidas(), res.Fallidas())
	for _, l := range res.Lecturas {
		if l.Err != nil {
			fmt.Fprintf(out, "  error  %s (%s): %v\n", l.Printer.Etiqueta(), l.Printer.IP, l.Err)
			continue
		}
		total := "sin total"
		if l.Reading.Total != nil {
			total = fmt.Sprintf("total %d", *l.Reading.Total)
		}
		fmt.Fprintf(out, "  ok     %s (%s): %s\n", l.Printer.Etiqueta(), l.Printer.IP, total)
		for _, a := range l.Reading.Avisos {
			fmt.Fprintf(out, "         aviso: %s\n", a)
		}
	}
	if res.CSVErr != nil {
		fmt.Fprintln(errOut, "Error:", res.CSVErr)
		return salidaGeneral
	}
	fmt.Fprintf(out, "CSV: %s\n", res.CSVPath)
	if res.Fallidas() > 0 {
		return salidaFallas
	}
	return salidaOK
}

// cargarConfig lee la configuración; si no existe, la crea con valores por defecto.
func cargarConfig(ruta string) (*config.Config, error) {
	if ruta == "" {
		var err error
		if ruta, err = config.DefaultPath(); err != nil {
			return nil, err
		}
	}
	cfg, creado, err := config.Load(ruta)
	if err != nil {
		return nil, err
	}
	if creado {
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("crear %s: %w", ruta, err)
		}
	}
	return cfg, nil
}
