# pagemeter

Lee por SNMP los contadores de impresión de impresoras de red y agrega cada escaneo a un CSV
acumulativo. Incluye una interfaz de consola y un modo sin interfaz para el Programador de tareas.

Por ahora solo tiene driver para **Brother**. Funciona en **Windows 10 y 11**.

## Compilar

Requiere Go 1.27 o posterior. No usa CGO y produce un solo `.exe`.

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o bin\pagemeter.exe .\cmd\pagemeter
go test ./...
```

## Habilitar SNMP en la impresora

En la web de la impresora: **Red → Protocolo → SNMP**. Debe permitir lectura con la community
configurada en `config.json` (por defecto `public`, SNMP v1).

## Uso

```powershell
pagemeter.exe                          # abre la interfaz
pagemeter.exe --scan                   # escanea las habilitadas, agrega al CSV y sale
pagemeter.exe --config D:\x\config.json
```

| Código de salida de `--scan` | Significado |
|---|---|
| 0 | Todas las impresoras se leyeron y el CSV se actualizó |
| 1 | Alguna impresora falló; el CSV se actualizó, con su fila en `estado=error` |
| 2 | Error de configuración o argumentos, ninguna impresora habilitada, o no se pudo escribir el CSV |

Si el CSV está abierto en Excel no se puede escribir y **las lecturas de ese escaneo no se guardan**.

Ejemplo para el Programador de tareas (diario a las 08:00):

```powershell
schtasks /Create /TN "pagemeter" /SC DAILY /ST 08:00 /TR "C:\pagemeter\pagemeter.exe --scan"
```

### Teclas

`1`–`4` cambian de pantalla, `Q` o `Ctrl+C` salen.

| Pantalla | Teclas |
|---|---|
| 1 Impresoras | `Espacio` marcar · `I` releer instaladas · `A` agregar por IP · `R` buscar en la red · `E` editar · `Supr` eliminar · `P` probar · `S` escanear |
| 2 Escanear | `S` escanear · `Esc` cancelar |
| 3 Último escaneo | `↑↓` impresora · `O` OIDs crudos · `Tab` cambiar panel · `S` escanear |
| 4 Historial | `Espacio`/`M` marcar dos escaneos · `D` diferencia · `F` filtrar · `Tab` cambiar panel · `R` recargar |
| Buscar en red | `Enter` elegir red · `Espacio` marcar · `G` agregar marcadas · `Esc` volver o cancelar |

## Configuración

`config.json` se crea junto al `.exe`. Una ruta relativa en `csv_path` se resuelve desde la carpeta
de `config.json`.

```json
{
  "community": "public",
  "snmp_version": "1",
  "timeout_ms": 2000,
  "retries": 1,
  "filtro": "Brother",
  "csv_path": "contadores.csv",
  "red_timeout_ms": 800,
  "red_concurrencia": 64,
  "printers": [
    {"nombre": "Brother DCP-L5650DN series", "ip": "192.168.1.104", "marca": "auto", "origen": "instalada", "habilitada": true}
  ]
}
```

- `snmp_version`: `1` o `2c`.
- `marca`: `auto` detecta el driver por sysDescr; también acepta `brother` o `generico`.
- `origen`: `instalada`, `manual` o `red`.

## Formato del CSV

UTF-8 con BOM, separado por comas, con el encabezado una sola vez. Solo se agregan filas: una por
impresora y escaneo.

| Columna | Contenido |
|---|---|
| `fecha` | `2006-01-02 15:04:05`, hora local |
| `escaneo_id` | Igual para todas las filas de un escaneo |
| `impresora`, `marca`, `ip`, `sysdescr` | Identificación |
| `estado` | `ok` o `error` |
| `error` | Motivo del error, o avisos si la lectura fue parcial |
| `total` | Contador de vida (Printer-MIB) |
| `pc_total`, `pc_color`, `pc_mono` | Impresión desde PC |
| `copia_total`, `copia_color`, `copia_mono` | Copias |
| `fax_total`, `fax_color`, `fax_mono` | Fax |
| `funciones_raw` | Desglose crudo por función, p. ej. `1=64002;3=64850;91=101` |
| `tamanos_raw` | Desglose crudo por tamaño de papel, mismo formato |

Una celda vacía significa que la impresora no reporta ese valor; no equivale a 0.

## Limitaciones y suposiciones

- Los códigos de función 1–9 de Brother cuadran con las sumas de impresoras reales. Los demás se
  muestran como "no documentado". Los nombres de tamaños de papel no están contrastados con
  documentación de Brother.
- La IP de un puerto WSD sale de propiedades que Windows guardó y **puede estar vieja**. La tecla `I`
  avisa si no responde, pero no cambia la IP de una impresora que ya está en la lista: edítala con `E`.
- La búsqueda de la IP WSD por nombre de modelo es un último recurso y no se ha probado con un equipo real.
- Que los hostnames `BRN…`/`BRW…` contengan la MAC es una convención observada, no documentada.

## Agregar otra marca

1. Crea `internal/vendors/<marca>/` con un tipo que implemente `counters.Driver`.
2. Regístralo en `registro()`, en `cmd/pagemeter/main.go`.

Las impresoras sin driver reconocido se leen con el driver genérico, que solo obtiene el total.
