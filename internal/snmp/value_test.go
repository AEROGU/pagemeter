package snmp

import (
	"context"
	"reflect"
	"testing"
)

func TestCompareOID(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.3.6.1.2", "1.3.6.1.10", -1}, // numérico, no lexicográfico
		{"1.3.6.1", "1.3.6.1.0", -1},
		{".1.3.6", "1.3.6", 0},
		{"1.3.7", "1.3.6.9", 1},
	}
	for _, c := range cases {
		if got := CompareOID(c.a, c.b); got != c.want {
			t.Errorf("CompareOID(%s,%s) = %d, se esperaba %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHasPrefixYSubOID(t *testing.T) {
	if !HasPrefix(".1.3.6.1.4", "1.3.6") || HasPrefix("1.3.61", "1.3.6") {
		t.Error("HasPrefix no respeta límites de componente")
	}
	sub, err := SubOID("1.2.3.21.1.3.8", "1.2.3.21.1")
	if err != nil || !reflect.DeepEqual(sub, []int{3, 8}) {
		t.Errorf("SubOID = %v, %v", sub, err)
	}
	if _, err := SubOID("1.2.4", "1.2.3"); err == nil {
		t.Error("SubOID fuera de rama debió fallar")
	}
}

func TestInt64(t *testing.T) {
	if n, ok := UintVar("1", 42).Int64(); !ok || n != 42 {
		t.Error("uint")
	}
	if _, ok := UintVar("1", 1<<63).Int64(); ok {
		t.Error("uint fuera de rango debió fallar")
	}
	if n, ok := StrVar("1", " 7 ").Int64(); !ok || n != 7 {
		t.Error("texto numérico")
	}
	if _, ok := StrVar("1", "Brother").Int64(); ok {
		t.Error("texto no numérico")
	}
}

func TestOctets(t *testing.T) {
	if got := octets([]byte("Brother NC-8300h\x00")); got != "Brother NC-8300h" {
		t.Errorf("texto = %q", got)
	}
	if got := octets([]byte{0x00, 0x1B, 0xA9}); got != "00 1B A9" {
		t.Errorf("binario = %q", got)
	}
}

func TestParseVersion(t *testing.T) {
	for _, s := range []string{"", "1", "2c", "v2c"} {
		if _, err := ParseVersion(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := ParseVersion("3"); err == nil {
		t.Error("v3 debió fallar")
	}
}

func TestMock(t *testing.T) {
	m := NewMock(IntVar("1.3.6.1.10", 10), IntVar("1.3.6.1.2", 2), StrVar("1.3.7", "x"))
	ctx := context.Background()
	w, err := m.Walk(ctx, "1.3.6")
	if err != nil || len(w) != 2 || w[0].OID != "1.3.6.1.2" || w[1].OID != "1.3.6.1.10" {
		t.Errorf("walk = %+v, %v", w, err)
	}
	g, _ := m.Get(ctx, "1.3.7", "9.9")
	if g[0].Str != "x" || g[1].Kind != KindNull {
		t.Errorf("get = %+v", g)
	}
	ctxC, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Walk(ctxC, "1"); err == nil {
		t.Error("contexto cancelado debió fallar")
	}
}
