package store

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// Append agrega filas al CSV. Si el archivo no existe o está vacío, escribe
// primero el BOM y el encabezado. Si existe con otro encabezado, no escribe nada
// y devuelve error, para no mezclar formatos.
func Append(path string, filas []Fila) error {
	if len(filas) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", path, explicarBloqueo(err))
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("leer datos de %s: %w", path, err)
	}
	nuevo := st.Size() == 0
	if !nuevo {
		if err := verificarEncabezado(f); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		// Si la última línea no termina en salto (archivo editado a mano), lo agrega.
		if err := asegurarSaltoFinal(f, st.Size()); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("posicionar al final de %s: %w", path, err)
	}

	// Se arma todo en memoria y se escribe de un golpe, para minimizar filas a medias.
	var buf bytes.Buffer
	if nuevo {
		buf.Write(bom)
	}
	w := csv.NewWriter(&buf)
	w.UseCRLF = true // Excel
	if nuevo {
		_ = w.Write(Encabezado)
	}
	for _, fl := range filas {
		_ = w.Write(fl.registro())
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("armar CSV: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("escribir en %s: %w", path, explicarBloqueo(err))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sincronizar %s: %w", path, err)
	}
	return nil
}

func (f Fila) registro() []string {
	r := []string{
		f.Fecha.Format(FormatoFecha), f.EscaneoID, f.Impresora, f.Marca, f.IP,
		limpiar(f.SysDescr), f.Estado, limpiar(f.Error),
	}
	for _, v := range f.Num {
		if v == nil {
			r = append(r, "")
		} else {
			r = append(r, strconv.FormatInt(*v, 10))
		}
	}
	return append(r, f.FuncionesRaw, f.TamanosRaw)
}

// limpiar quita saltos de línea (sysDescr a veces los trae) para que cada fila
// ocupe una sola línea al abrir el CSV con otras herramientas.
func limpiar(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func verificarEncabezado(f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	linea, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("leer encabezado: %w", err)
	}
	linea = strings.TrimRight(strings.TrimPrefix(linea, string(bom)), "\r\n")
	if linea != strings.Join(Encabezado, ",") {
		return fmt.Errorf("el encabezado del CSV no coincide con el formato actual; renómbralo o usa otro csv_path")
	}
	return nil
}

func asegurarSaltoFinal(f *os.File, size int64) error {
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, size-1); err != nil {
		return fmt.Errorf("leer final del archivo: %w", err)
	}
	if b[0] == '\n' {
		return nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err := f.Write([]byte("\r\n"))
	return err
}

// ERROR_SHARING_VIOLATION / ERROR_LOCK_VIOLATION de Windows.
const (
	errSharingViolation syscall.Errno = 32
	errLockViolation    syscall.Errno = 33
)

func explicarBloqueo(err error) error {
	// Quien llama ya incluye la ruta en su mensaje; así no sale repetida.
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	if errors.Is(err, errSharingViolation) || errors.Is(err, errLockViolation) {
		return fmt.Errorf("%w (¿está abierto en Excel? ciérralo y vuelve a intentar)", err)
	}
	return err
}

// Leer carga todo el CSV. Devuelve las filas válidas y avisos por las líneas que
// no se pudieron interpretar. Si el archivo no existe, devuelve lista vacía sin error.
func Leer(path string) (filas []Fila, avisos []string, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("leer %s: %w", path, explicarBloqueo(err))
	}
	return Parse(bytes.NewReader(bytes.TrimPrefix(data, bom)))
}

// Parse interpreta un CSV (sin BOM). Las columnas se ubican por nombre, así que
// tolera columnas en otro orden o extra.
func Parse(r io.Reader) (filas []Fila, avisos []string, err error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	enc, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("leer encabezado del CSV: %w", err)
	}
	idx := map[string]int{}
	for i, h := range enc {
		idx[strings.TrimSpace(strings.TrimPrefix(h, string(bom)))] = i
	}
	for _, req := range []string{"fecha", "escaneo_id", "impresora", "ip", "estado"} {
		if _, ok := idx[req]; !ok {
			return nil, nil, fmt.Errorf("al CSV le falta la columna %q", req)
		}
	}
	campo := func(rec []string, nombre string) string {
		if i, ok := idx[nombre]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		linea, _ := cr.FieldPos(0)
		if err != nil {
			avisos = append(avisos, fmt.Sprintf("línea %d: %v", linea, err))
			continue
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		fecha, err := time.ParseInLocation(FormatoFecha, campo(rec, "fecha"), time.Local)
		if err != nil {
			avisos = append(avisos, fmt.Sprintf("línea %d: fecha inválida %q", linea, campo(rec, "fecha")))
			continue
		}
		f := Fila{
			Fecha:        fecha,
			EscaneoID:    campo(rec, "escaneo_id"),
			Impresora:    campo(rec, "impresora"),
			Marca:        campo(rec, "marca"),
			IP:           campo(rec, "ip"),
			SysDescr:     campo(rec, "sysdescr"),
			Estado:       campo(rec, "estado"),
			Error:        campo(rec, "error"),
			FuncionesRaw: campo(rec, "funciones_raw"),
			TamanosRaw:   campo(rec, "tamanos_raw"),
		}
		malo := false
		for i, col := range ColumnasNum {
			s := strings.TrimSpace(campo(rec, col))
			if s == "" {
				continue
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				avisos = append(avisos, fmt.Sprintf("línea %d: %s no es número (%q)", linea, col, s))
				malo = true
				break
			}
			f.Num[i] = &n
		}
		if !malo {
			filas = append(filas, f)
		}
	}
	return filas, avisos, nil
}
