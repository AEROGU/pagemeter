package snmp

import (
	"context"
	"fmt"
)

// OIDs estándar (MIB-II, Printer-MIB, HOST-RESOURCES-MIB), válidos para cualquier marca.
const (
	OIDSysDescr = "1.3.6.1.2.1.1.1.0"
	// prtMarkerLifeCount: contador de vida del motor de impresión. Columna de tabla
	// indexada por (hrDeviceIndex, prtMarkerIndex), por eso se recorre con walk.
	OIDPrtMarkerLifeCount = "1.3.6.1.2.1.43.10.2.1.4"
	// hrDeviceDescr: descripción de cada dispositivo. En impresoras suele traer el modelo.
	// SUPOSICIÓN: no comprobado con equipos reales; puede no existir.
	OIDHrDeviceDescr = "1.3.6.1.2.1.25.3.2.1.3"
)

// SysDescr lee sysDescr.0. Es la prueba básica de "¿responde SNMP?".
func SysDescr(ctx context.Context, c Client) (string, error) {
	vs, err := c.Get(ctx, OIDSysDescr)
	if err != nil {
		return "", fmt.Errorf("leer sysDescr: %w", err)
	}
	if len(vs) == 0 || vs[0].Kind == KindNull {
		return "", fmt.Errorf("leer sysDescr: el equipo no lo reporta")
	}
	return vs[0].String(), nil
}

// LifeCount lee el primer prtMarkerLifeCount. Devuelve nil (sin error) si el
// equipo responde pero no implementa Printer-MIB. Además regresa las variables leídas.
func LifeCount(ctx context.Context, c Client) (*int64, []Var, error) {
	vs, err := c.Walk(ctx, OIDPrtMarkerLifeCount)
	if err != nil {
		return nil, nil, fmt.Errorf("leer prtMarkerLifeCount: %w", err)
	}
	for _, v := range vs {
		if n, ok := v.Int64(); ok {
			return &n, vs, nil
		}
	}
	return nil, vs, nil
}

// DeviceModel intenta obtener el modelo desde hrDeviceDescr (primer valor no vacío).
// SUPOSICIÓN: ver OIDHrDeviceDescr. Devuelve "" si no hay dato.
func DeviceModel(ctx context.Context, c Client) string {
	vs, err := c.Walk(ctx, OIDHrDeviceDescr)
	if err != nil {
		return ""
	}
	for _, v := range vs {
		if s := v.String(); s != "" {
			return s
		}
	}
	return ""
}
