// Package buffer implementa un ring buffer en disco acotado (README §4, §5).
//
// Propósito: si el backend no responde, el payload se guarda aquí en vez de
// perderse. Al recuperar conexión se drena con backoff. Tamaño ACOTADO: si se
// llena, se descarta lo más viejo (nunca RAM ni disco ilimitados).
//
// Implementación: fichero JSONL (una línea = un payload JSON). Sobre ese
// fichero se mantiene un índice lógico head/tail计数 acotado; la rotación es
// truncado + reescritura. Creación/lectura tolerantes: si el fichero no existe
// parte de cero; si está corrupto, se arranca limpio.
package buffer

import (
	"bufio"
	"encoding/json"
	"io"
	"os"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type RingBuffer struct {
	path     string
	maxItems int
}

func NewRingBuffer(path string, maxItems int) *RingBuffer {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &RingBuffer{path: path, maxItems: maxItems}
}

// readLines devuelve todas las líneas del fichero (cada una un payload).
// Si el fichero no existe o está corrupto, devuelve slice vacío y nil.
func readLines(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		// copiar: sc.Bytes reutiliza el buffer interno
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}
	if err := sc.Err(); err != nil {
		// recuperar lo que podamos: si el fichero está corrupto parcialmente
		// se conservan las líneas válidas leídas hasta aquí.
		return lines, nil
	}
	return lines, nil
}

// writeLines reescribe el fichero con `lines` en el mismo orden.
func writeLines(path string, lines [][]byte) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, l := range lines {
		_, _ = w.Write(l)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Push encola un payload. Si el buffer está lleno, descarta el más viejo.
func (b *RingBuffer) Push(p *payload.Payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	lines, err := readLines(b.path)
	if err != nil {
		return err
	}
	lines = append(lines, data)
	// si supera el máximo, descarta los más viejos (cabeza) quedándonos con
	// los últimos maxItems.
	if len(lines) > b.maxItems {
		lines = lines[len(lines)-b.maxItems:]
	}
	return writeLines(b.path, lines)
}

// Pop devuelve el payload más viejo y lo elimina. io.EOF = buffer vacío.
func (b *RingBuffer) Pop() (*payload.Payload, error) {
	lines, err := readLines(b.path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, io.EOF
	}
	var p payload.Payload
	if err := json.Unmarshal(lines[0], &p); err != nil {
		// Línea corrupta: la descartamos para no quedar trabados, retornando
		// el error para que DrainController lo registre y reintentar Pop().
		_ = writeLines(b.path, lines[1:])
		return nil, err
	}
	if err := writeLines(b.path, lines[1:]); err != nil {
		return nil, err
	}
	return &p, nil
}

// Len devuelve el número de payloads pendientes.
func (b *RingBuffer) Len() int {
	lines, _ := readLines(b.path)
	return len(lines)
}