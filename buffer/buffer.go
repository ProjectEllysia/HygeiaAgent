// Package buffer implementa un ring buffer en disco acotado (README §4, §5).
//
// Propósito: si el backend no responde, el payload se guarda aquí en vez de
// perderse. Al recuperar conexión se drena con backoff. Tamaño ACOTADO: si se
// llena, se descarta lo más viejo (nunca RAM ni disco ilimitados).
package buffer

import (
	"io"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type RingBuffer struct {
	path     string
	maxItems int
	// TODO: estado del ring (fichero + índices head/tail). Una implementación
	// sencilla es JSONL: una línea = un payload, con un contador de líneas y
	// rotación cuando se supera maxItems.
}

func NewRingBuffer(path string, maxItems int) *RingBuffer {
	return &RingBuffer{path: path, maxItems: maxItems}
}

// Push encola un payload. Si el buffer está lleno, descarta el más viejo.
func (b *RingBuffer) Push(p *payload.Payload) error {
	// TODO: serializar p a JSON y appendar; mantener el límite maxItems.
	return nil
}

// Pop devuelve el payload más viejo y lo elimina. io.EOF = buffer vacío.
func (b *RingBuffer) Pop() (*payload.Payload, error) {
	// TODO: leer/eliminar la línea más vieja.
	return nil, io.EOF
}

func (b *RingBuffer) Len() int {
	// TODO: número de payloads pendientes.
	return 0
}
