package discovery

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// TestInstaladasReal corre el script de PowerShell en esta PC. Sólo con PAGEMETER_REAL=1.
// Con PAGEMETER_DUMP=<archivo> guarda además el JSON crudo.
func TestInstaladasReal(t *testing.T) {
	if os.Getenv("PAGEMETER_REAL") == "" {
		t.Skip("define PAGEMETER_REAL=1 para probar contra esta PC")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ini := time.Now()
	out, err := RunPowerShell(ctx, scriptInstaladas)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PowerShell tardó %v, %d bytes", time.Since(ini).Round(time.Millisecond), len(out))
	if f := os.Getenv("PAGEMETER_DUMP"); f != "" {
		_ = os.WriteFile(f, out, 0o644)
	}
	c, err := ParseCrudo(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("colas: %d, dispositivos PnP: %d, vecinos ARP: %d", len(c.Printers), len(c.Devices), len(c.Neighbors))
	ins := Resolver(ctx, c, "Brother", nil2(t))
	Verificar(ctx, ins, snmp.Params{Timeout: 2 * time.Second, Retries: 1}, nil)
	for _, in := range ins {
		t.Logf("%-45s | %-15s | %s%s | responde=%v", in.Nombre, in.IP, in.Metodo, in.Error, in.Responde)
	}
}

func nil2(t *testing.T) LookupFunc {
	return func(ctx context.Context, h string) ([]string, error) {
		ini := time.Now()
		a, err := defaultLookup(ctx, h)
		t.Logf("lookup %s → %v %v (%v)", h, a, err, time.Since(ini).Round(time.Millisecond))
		return a, err
	}
}

// TestBarridoReal barre una red de verdad. Sólo con PAGEMETER_RED=<cidr>;
// PAGEMETER_LOCAL=<ip> enlaza el socket a esa IP local.
func TestBarridoReal(t *testing.T) {
	cidr := os.Getenv("PAGEMETER_RED")
	if cidr == "" {
		t.Skip("define PAGEMETER_RED=192.168.1.0/24 para barrer una red real")
	}
	red, err := ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	hosts, _ := Hosts(red)
	ini := time.Now()
	res, err := Barrer(context.Background(), hosts, OpcionesBarrido{
		Base:      snmp.Params{Community: "public", Version: "1"},
		LocalIP:   os.Getenv("PAGEMETER_LOCAL"),
		Candidata: func(sd string) bool { return strings.Contains(strings.ToLower(sd), "brother") },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d hosts en %v, respondieron %d", len(hosts), time.Since(ini).Round(time.Millisecond), len(res))
	for _, r := range res {
		t.Logf("%-15s | %-60.60s | modelo: %s", r.IP, r.SysDescr, r.Modelo)
	}
}
