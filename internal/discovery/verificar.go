package discovery

import (
	"context"
	"sync"

	"github.com/AEROGU/pagemeter/internal/snmp"
)

// Verificar pide sysDescr a cada impresora con IP, en paralelo, y marca si responde.
//
// Hace falta porque la IP de un puerto WSD sale de propiedades PnP guardadas al
// instalar la impresora y puede estar vieja: Windows puede seguir mostrando una
// IP que ya no responde mientras el barrido de red encuentra el modelo en otra.
func Verificar(ctx context.Context, ins []Instalada, base snmp.Params, dial snmp.Dialer) {
	if dial == nil {
		dial = snmp.Dial
	}
	var wg sync.WaitGroup
	for i := range ins {
		if ins[i].IP == "" {
			continue
		}
		wg.Add(1)
		go func(in *Instalada) {
			defer wg.Done()
			p := base
			p.Target = in.IP
			in.Verificada = true
			c, err := dial(p)
			if err != nil {
				return
			}
			defer c.Close()
			if sd, err := snmp.SysDescr(ctx, c); err == nil {
				in.Responde, in.SysDescr = true, sd
			}
		}(&ins[i])
	}
	wg.Wait()
}
