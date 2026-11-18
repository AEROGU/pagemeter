package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

// scriptInstaladas junta en una sola llamada todo lo que hace falta; la lógica
// de resolución está en Go (Resolver) para poder probarla con fixtures.
//
// Para puertos WSD-<guid> trae las propiedades PnP del dispositivo
// SWD\PRINTENUM\WSD-<guid>, las de su padre (en esta PC resultó ser
// SWD\IPP\<uuid-que-termina-en-MAC>, con propiedades http(s)://IP/ipp/print)
// y, como respaldo, las de los dispositivos SWD\DAFWSDPROVIDER* y SWD\IPP\*.
const scriptInstaladas = `
$ErrorActionPreference = 'SilentlyContinue'
function Get-Props([string]$id) {
  $props = @(Get-PnpDeviceProperty -InstanceId $id | ForEach-Object {
    $vals = @(@($_.Data) | Where-Object { $null -ne $_ } | ForEach-Object { "$_" })
    $keep = $_.KeyName -match 'Parent|FriendlyName|DEVPKEY_NAME|Ip' -or ($vals -match '\d{1,3}(\.\d{1,3}){3}')
    if ($keep -and $vals.Count) { [pscustomobject]@{ Key = $_.KeyName; Values = $vals } }
  })
  [pscustomobject]@{ Id = $id; Props = $props }
}
$printers = @(Get-Printer | ForEach-Object {
  $port = Get-PrinterPort -Name $_.PortName
  [pscustomobject]@{ Name = $_.Name; PortName = $_.PortName; DriverName = $_.DriverName; HostAddress = "$($port.PrinterHostAddress)" }
})
$devices = @()
$wsd = @($printers | Where-Object { $_.PortName -like 'WSD-*' })
if ($wsd.Count) {
  $ids = [System.Collections.Generic.List[string]]::new()
  foreach ($p in $wsd) { $ids.Add('SWD\PRINTENUM\' + $p.PortName) }
  foreach ($d in @(Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -like 'SWD\DAFWSDPROVIDER*' -or $_.InstanceId -like 'SWD\IPP\*' })) {
    $ids.Add($d.InstanceId)
  }
  $seen = @{}
  $i = 0
  while ($i -lt $ids.Count) {
    $id = $ids[$i]; $i++
    if ($seen.ContainsKey($id.ToUpper())) { continue }
    $seen[$id.ToUpper()] = $true
    $dev = Get-Props $id
    $devices += $dev
    if ($id -like 'SWD\PRINTENUM\*') {
      foreach ($pr in $dev.Props) { if ($pr.Key -eq 'DEVPKEY_Device_Parent') { foreach ($v in $pr.Values) { $ids.Add($v) } } }
    }
  }
}
$neighbors = @(Get-NetNeighbor -AddressFamily IPv4 | Where-Object { $_.LinkLayerAddress -and $_.LinkLayerAddress -ne '00-00-00-00-00-00' } |
  ForEach-Object { [pscustomobject]@{ IP = $_.IPAddress; MAC = $_.LinkLayerAddress } })
[pscustomobject]@{ Printers = $printers; Devices = $devices; Neighbors = $neighbors } | ConvertTo-Json -Depth 6 -Compress
`

// Crudo es lo que devuelve el script de PowerShell.
type Crudo struct {
	Printers  Lista[ImpresoraWin] `json:"Printers"`
	Devices   Lista[Dispositivo]  `json:"Devices"`
	Neighbors Lista[Vecino]       `json:"Neighbors"`
}

// ImpresoraWin es una cola de impresión de Windows (Get-Printer + Get-PrinterPort).
type ImpresoraWin struct {
	Name        string `json:"Name"`
	PortName    string `json:"PortName"`
	DriverName  string `json:"DriverName"`
	HostAddress string `json:"HostAddress"`
}

// Dispositivo es un dispositivo PnP con sus propiedades (ya filtradas).
type Dispositivo struct {
	ID    string           `json:"Id"`
	Props Lista[Propiedad] `json:"Props"`
}

// Propiedad es una propiedad PnP.
type Propiedad struct {
	Key    string       `json:"Key"`
	Values Lista[Texto] `json:"Values"`
}

// Vecino es una entrada de la caché ARP (Get-NetNeighbor).
type Vecino struct {
	IP  string `json:"IP"`
	MAC string `json:"MAC"`
}

// Instalada es una impresora instalada con su IP resuelta (o el motivo de no tenerla).
type Instalada struct {
	Nombre string
	Puerto string
	Driver string
	IP     string // vacío = sin IP
	Metodo string // cómo se obtuvo la IP
	Error  string // por qué no se obtuvo

	// Llenados por Verificar.
	Verificada bool   // se intentó SNMP
	Responde   bool   // contestó sysDescr
	SysDescr   string // lo que contestó
}

// LeerCrudo ejecuta el script y parsea su salida.
func LeerCrudo(ctx context.Context) (Crudo, error) {
	out, err := RunPowerShell(ctx, scriptInstaladas)
	if err != nil {
		return Crudo{}, fmt.Errorf("listar impresoras instaladas: %w", err)
	}
	return ParseCrudo(out)
}

// ParseCrudo interpreta el JSON del script.
func ParseCrudo(b []byte) (Crudo, error) {
	var c Crudo
	if len(strings.TrimSpace(string(b))) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("interpretar JSON de PowerShell: %w", err)
	}
	return c, nil
}

// Instaladas lista las impresoras instaladas que coinciden con el filtro y resuelve su IP.
func Instaladas(ctx context.Context, filtro string) ([]Instalada, error) {
	c, err := LeerCrudo(ctx)
	if err != nil {
		return nil, err
	}
	return Resolver(ctx, c, filtro, defaultLookup), nil
}

// LookupFunc resuelve un hostname (inyectable en tests).
type LookupFunc func(ctx context.Context, host string) ([]string, error)

var (
	reIPv4     = regexp.MustCompile(`(?:^|[^\d.])(\d{1,3}(?:\.\d{1,3}){3})(?:$|[^\d.])`) // también "IP_10.0.0.7"
	reURLIPv4  = regexp.MustCompile(`https?://(\d{1,3}(?:\.\d{1,3}){3})`)
	reMACNom   = regexp.MustCompile(`\[([0-9A-Fa-f]{12})\]`)
	reHostBR   = regexp.MustCompile(`(?i)^BR[NW]([0-9A-F]{12})$`)
	reMACFinal = regexp.MustCompile(`(?i)([0-9A-F]{12})$`)
)

// Resolver aplica el filtro y obtiene la IP de cada impresora, en este orden:
//  1. PrinterHostAddress del puerto (IP, o hostname resuelto por DNS/NetBIOS;
//     si no resuelve y es un hostname Brother BRN/BRW+MAC, se busca esa MAC en ARP);
//  2. puerto WSD-*: URL http(s)://IP en las propiedades PnP del dispositivo del
//     puerto o de su padre; si no, la MAC al final del id del padre en ARP; si no,
//     un dispositivo WSD/IPP cuyo nombre contenga el modelo;
//  3. una IP escrita en el nombre del puerto (p. ej. IP_192.168.1.5);
//  4. sufijo [MAC] en el nombre de la impresora, buscado en ARP.
//
// Excluye las que contengan "FAX" y deduplica por IP (gana la de nombre más corto,
// así "Brother X series" le gana a "Brother X series (Copiar 1)").
func Resolver(ctx context.Context, c Crudo, filtro string, lookup LookupFunc) []Instalada {
	filtro = strings.ToLower(strings.TrimSpace(filtro))
	// MAC → IP. Fuentes: caché ARP y dispositivos PnP cuyo id termina en la MAC y
	// traen una URL con IP (p. ej. SWD\IPP\e3248000-...-0011223344aa). Así una cola
	// con puerto BRN<MAC> que no resuelve se empareja con la cola WSD de la misma impresora.
	macs := map[string]macIP{}
	for _, d := range c.Devices {
		if m := reMACFinal.FindStringSubmatch(d.ID); m != nil {
			if a := urlIPv4(d); a != "" {
				macs[normMAC(m[1])] = macIP{a, "dispositivos PnP"}
			}
		}
	}
	for _, n := range c.Neighbors {
		macs[normMAC(n.MAC)] = macIP{n.IP, "caché ARP"} // la caché ARP es más reciente
	}
	devs := map[string]Dispositivo{}
	for _, d := range c.Devices {
		devs[strings.ToUpper(d.ID)] = d
	}

	var ps []ImpresoraWin
	for _, p := range c.Printers {
		nom := strings.ToLower(p.Name)
		if strings.Contains(nom, "fax") {
			continue
		}
		if filtro != "" && !strings.Contains(nom, filtro) {
			continue
		}
		ps = append(ps, p)
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if len(ps[i].Name) != len(ps[j].Name) {
			return len(ps[i].Name) < len(ps[j].Name)
		}
		return ps[i].Name < ps[j].Name
	})

	var out []Instalada
	vistas := map[string]bool{}
	for _, p := range ps {
		in := Instalada{Nombre: p.Name, Puerto: p.PortName, Driver: p.DriverName}
		in.IP, in.Metodo, in.Error = resolverUna(ctx, p, lookup, macs, devs, c.Devices)
		if in.IP != "" {
			if vistas[in.IP] {
				continue
			}
			vistas[in.IP] = true
		}
		out = append(out, in)
	}
	return out
}

type macIP struct{ ip, fuente string }

func resolverUna(ctx context.Context, p ImpresoraWin, lookup LookupFunc, macs map[string]macIP,
	devs map[string]Dispositivo, todos []Dispositivo) (ip, metodo, motivo string) {

	var motivos []string
	host := strings.TrimSpace(p.HostAddress)

	// 1. Dirección del puerto.
	if host != "" {
		if esIPv4(host) {
			return host, "puerto TCP/IP", ""
		}
		if addrs, err := lookup(ctx, host); err == nil {
			for _, a := range addrs {
				if esIPv4(a) {
					return a, "hostname " + host, ""
				}
			}
			motivos = append(motivos, fmt.Sprintf("%s no tiene IPv4", host))
		} else {
			motivos = append(motivos, fmt.Sprintf("%s no resuelve", host))
		}
		// SUPOSICIÓN: los hostnames Brother BRN/BRW llevan la MAC de la tarjeta de red.
		if m := reHostBR.FindStringSubmatch(host); m != nil {
			if a, ok := macs[normMAC(m[1])]; ok {
				return a.ip, "MAC del hostname " + host + " en " + a.fuente, ""
			}
			motivos = append(motivos, "su MAC no está en la caché ARP")
		}
	}

	// 2. Puerto WSD.
	if strings.HasPrefix(strings.ToUpper(p.PortName), "WSD-") {
		id := strings.ToUpper(`SWD\PRINTENUM\` + p.PortName)
		if d, ok := devs[id]; ok {
			if a := urlIPv4(d); a != "" {
				return a, "WSD (propiedades PnP)", ""
			}
			for _, padre := range valores(d, "DEVPKEY_Device_Parent") {
				if dp, ok := devs[strings.ToUpper(padre)]; ok {
					if a := urlIPv4(dp); a != "" {
						return a, "WSD (dispositivo padre " + tipoDispositivo(padre) + ")", ""
					}
				}
				if m := reMACFinal.FindStringSubmatch(padre); m != nil {
					if a, ok := macs[normMAC(m[1])]; ok {
						return a.ip, "WSD (MAC del dispositivo padre en " + a.fuente + ")", ""
					}
				}
			}
		}
		// SUPOSICIÓN (no probado): coincidencia por modelo en el nombre del dispositivo WSD.
		if modelo := Modelo(p.Name); modelo != "" {
			for _, d := range todos {
				for _, fn := range append(valores(d, "DEVPKEY_Device_FriendlyName"), valores(d, "DEVPKEY_NAME")...) {
					if strings.Contains(strings.ToLower(fn), strings.ToLower(modelo)) {
						if a := urlIPv4(d); a != "" {
							return a, "WSD (por nombre de modelo, sin verificar)", ""
						}
						if a := ipEnPropiedadIP(d); a != "" {
							return a, "WSD (por nombre de modelo, sin verificar)", ""
						}
					}
				}
			}
		}
		motivos = append(motivos, "no se encontró la IP del puerto WSD")
	}

	// 3. IP en el nombre del puerto.
	if m := reIPv4.FindStringSubmatch(p.PortName); m != nil && esIPv4(m[1]) {
		return m[1], "nombre del puerto", ""
	}

	// 4. [MAC] en el nombre.
	if m := reMACNom.FindStringSubmatch(p.Name); m != nil {
		if a, ok := macs[normMAC(m[1])]; ok {
			return a.ip, "MAC del nombre en " + a.fuente, ""
		}
		motivos = append(motivos, "la MAC del nombre no está en la caché ARP")
	}

	if len(motivos) == 0 {
		motivos = append(motivos, fmt.Sprintf("el puerto %s no es de red", p.PortName))
	}
	return "", "", "sin IP: " + strings.Join(motivos, "; ")
}

// Modelo extrae el modelo del nombre de la cola:
// "Brother DCP-L5650DN series [b4220000aa01]" → "DCP-L5650DN".
func Modelo(nombre string) string {
	s := reMACNom.ReplaceAllString(nombre, "")
	s = regexp.MustCompile(`\(.*?\)`).ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`(?i)^brother\s+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)\s+(series|printer)\b.*$`).ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func valores(d Dispositivo, key string) []string {
	var out []string
	for _, p := range d.Props {
		if p.Key == key {
			for _, v := range p.Values {
				out = append(out, string(v))
			}
		}
	}
	return out
}

func urlIPv4(d Dispositivo) string {
	for _, p := range d.Props {
		for _, v := range p.Values {
			if m := reURLIPv4.FindStringSubmatch(string(v)); m != nil && esIPv4(m[1]) {
				return m[1]
			}
		}
	}
	return ""
}

func ipEnPropiedadIP(d Dispositivo) string {
	for _, p := range d.Props {
		if !strings.Contains(p.Key, "Ip") {
			continue
		}
		for _, v := range p.Values {
			if s := strings.TrimSpace(string(v)); esIPv4(s) {
				return s
			}
		}
	}
	return ""
}

func tipoDispositivo(id string) string {
	if i := strings.LastIndex(id, `\`); i > 0 {
		return id[:i]
	}
	return id
}

func esIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

func normMAC(s string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", ":", "", ".", "").Replace(strings.TrimSpace(s)))
}

func defaultLookup(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}
