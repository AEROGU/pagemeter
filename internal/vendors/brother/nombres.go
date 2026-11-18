package brother

import "fmt"

// Códigos de la tabla de funciones (21). Comprobados con el fixture:
// 5+7 = 1 y 4+9 = 3 en una impresora a color. Los demás códigos (10–20, 91–96)
// no están documentados y se muestran como desconocidos.
const (
	FuncPCTotal    = 1
	FuncFaxTotal   = 2
	FuncCopiaTotal = 3
	FuncCopiaColor = 4
	FuncPCColor    = 5
	FuncFaxColor   = 6
	FuncPCMono     = 7
	FuncFaxMono    = 8
	FuncCopiaMono  = 9
)

var nombresFuncion = map[int]string{
	FuncPCTotal: "PC: total", FuncPCColor: "PC: color", FuncPCMono: "PC: mono",
	FuncCopiaTotal: "Copia: total", FuncCopiaColor: "Copia: color", FuncCopiaMono: "Copia: mono",
	FuncFaxTotal: "Fax: total", FuncFaxColor: "Fax: color", FuncFaxMono: "Fax: mono",
}

// Códigos de tamaño de papel (tabla 1). Tomados del script Get-BrotherCounters.ps1;
// no verificados contra documentación de Brother.
var nombresTamano = map[int]string{
	1: "Executive", 2: "Carta", 3: "Oficio (Legal)", 24: "A6", 25: "A5", 26: "A4", 27: "A3",
	45: "B5 JIS", 46: "B4 JIS", 80: "Monarch", 81: "Com-10", 90: "DL", 91: "C5", 99: "B6", 100: "B5",
	890: "Ledger", 891: "A3+", 892: "Carta (borde corto)", 893: "A4 (borde corto)", 894: "A4 largo",
	895: "Executive (borde corto)", 896: "B5 ISO (borde corto)", 897: "Personalizado",
	898: "A4 / Carta", 899: "B5 / Executive", 900: "Sobres", 901: "DL-L", 902: "Hagaki", 903: "Folio",
	908: "Definido por usuario", 909: "Oficio / A4 largo", 910: "B6 / A5 / A6",
	920: "3x5 in", 921: "Sobre Y4", 924: "B6 JIS", 999: "Otro",
}

// NombreFuncion devuelve el nombre del código y si es conocido.
func NombreFuncion(cod int) (string, bool) { return nombre(nombresFuncion, cod) }

// NombreTamano devuelve el nombre del código de tamaño y si es conocido.
func NombreTamano(cod int) (string, bool) { return nombre(nombresTamano, cod) }

func nombre(m map[int]string, cod int) (string, bool) {
	if n, ok := m[cod]; ok {
		return n, true
	}
	return fmt.Sprintf("Código %d (no documentado)", cod), false
}
