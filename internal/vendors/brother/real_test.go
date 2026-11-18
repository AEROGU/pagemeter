package brother

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AEROGU/pagemeter/internal/counters"
	"github.com/AEROGU/pagemeter/internal/snmp"
)

// TestImpresoraReal lee una impresora de verdad. Sólo corre si se define
// PAGEMETER_IP, p. ej.:  $env:PAGEMETER_IP="192.168.1.104"; go test -run Real -v ./internal/vendors/brother/
func TestImpresoraReal(t *testing.T) {
	ip := os.Getenv("PAGEMETER_IP")
	if ip == "" {
		t.Skip("define PAGEMETER_IP para probar contra una impresora real")
	}
	c, err := snmp.Dial(snmp.Params{Target: ip, Version: os.Getenv("PAGEMETER_VER"), Timeout: 2 * time.Second, Retries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, err := counters.NuevoRegistro(Driver{}).Leer(ctx, c, counters.MarcaAuto)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sysDescr: %s | marca: %s | total: %v", r.SysDescr, r.Marca, fmtP(r.Total))
	t.Logf("pc=%s/%s/%s copia=%s/%s/%s fax=%s", fmtP(r.PC.Total), fmtP(r.PC.Color), fmtP(r.PC.Mono),
		fmtP(r.Copia.Total), fmtP(r.Copia.Color), fmtP(r.Copia.Mono), fmtP(r.Fax.Total))
	t.Logf("funciones_raw: %s", counters.FormatRaw(r.Funciones))
	t.Logf("tamanos_raw: %s", counters.FormatRaw(r.Tamanos))
	t.Logf("avisos: %v | crudos: %d", r.Avisos, len(r.Crudos))
}
