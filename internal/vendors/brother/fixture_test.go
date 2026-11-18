package brother

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// cargarFixture lee testdata/Salida.txt (salida de Get-BrotherCounters.ps1 -Todo
// sobre dos impresoras) y devuelve un walk por impresora. Cada sección empieza
// con la tabla "Indice Paginas" (prtMarkerLifeCount) seguida de la tabla "Oid Valor".
func cargarFixture(t *testing.T) [][]snmp.Var {
	t.Helper()
	f, err := os.Open("testdata/Salida.txt")
	if err != nil {
		t.Fatalf("abrir fixture: %v", err)
	}
	defer f.Close()

	var out [][]snmp.Var
	enIndice := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		campos := strings.Fields(line)
		switch {
		case len(campos) == 0 || strings.HasPrefix(line, "---"):
			continue
		case campos[0] == "Indice":
			out = append(out, nil)
			enIndice = true
			continue
		case campos[0] == "Oid":
			enIndice = false
			continue
		}
		if len(out) == 0 || len(campos) < 2 {
			t.Fatalf("línea inesperada en fixture: %q", line)
		}
		val := strings.Join(campos[1:], " ")
		oid := campos[0]
		if enIndice {
			oid = snmp.OIDPrtMarkerLifeCount + "." + oid
		}
		var v snmp.Var
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			v = snmp.IntVar(oid, n)
		} else {
			v = snmp.StrVar(oid, val)
		}
		out[len(out)-1] = append(out[len(out)-1], v)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("leer fixture: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("se esperaban 2 impresoras en el fixture, hay %d", len(out))
	}
	return out
}

// soloRama filtra las variables bajo root.
func soloRama(vs []snmp.Var, root string) []snmp.Var {
	var out []snmp.Var
	for _, v := range vs {
		if snmp.HasPrefix(v.OID, root) {
			out = append(out, v)
		}
	}
	return out
}
