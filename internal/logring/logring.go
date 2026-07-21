// Package logring retiene las últimas N líneas escritas en un buffer
// acotado en memoria, para exponerlas por el canal de control (§11.4) sin
// depender de que quien depura encuentre el fichero de log en disco (plan
// §12.2, Tier 3: endpoint de depuración local).
package logring

import (
	"bytes"
	"sync"
)

// Buffer es un io.Writer que retiene las últimas `capacity` líneas
// escritas, descartando las más viejas — el mismo principio de "acotado,
// nunca crece sin límite" que el ring buffer en disco (README §5), aquí
// para texto en memoria en vez de payloads. Se conecta EN PARALELO al
// destino real de los logs (vía io.MultiWriter), nunca como único destino:
// si el proceso muere, este buffer se pierde con él.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	lines    []string
	partial  []byte // cola de bytes sin un '\n' todavía
}

// New crea un Buffer con capacidad para `capacity` líneas.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 20
	}
	return &Buffer{capacity: capacity}
}

// Write implementa io.Writer. slog escribe cada entrada ya con su '\n'
// final en una sola llamada, pero el trocéo por líneas se hace de forma
// general por si el escritor real hiciera varias escrituras parciales.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.partial = append(b.partial, p...)
	for {
		i := bytes.IndexByte(b.partial, '\n')
		if i < 0 {
			break
		}
		b.push(string(b.partial[:i]))
		b.partial = b.partial[i+1:]
	}
	return len(p), nil
}

func (b *Buffer) push(line string) {
	b.lines = append(b.lines, line)
	if len(b.lines) > b.capacity {
		b.lines = b.lines[len(b.lines)-b.capacity:]
	}
}

// Lines devuelve una copia de las líneas retenidas, de más vieja a más
// reciente.
func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}
