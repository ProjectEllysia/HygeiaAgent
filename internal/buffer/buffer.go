// Package buffer implementa un ring buffer en disco acotado (README §4, §5).
//
// Propósito: si el backend no responde, el payload se guarda aquí en vez de
// perderse. Al recuperar conexión se drena con backoff. Tamaño ACOTADO: si se
// llena, se descarta lo más viejo (nunca RAM ni disco ilimitados).
//
// Implementación: fichero JSONL (una línea = un payload JSON). Creación y
// lectura tolerantes: si el fichero no existe parte de cero; si una línea está
// corrupta, se descarta sin trabar el resto.
//
// # Coste de las operaciones
//
// La versión anterior leía y reescribía el fichero ENTERO en cada operación,
// incluida Len(). Con el buffer lleno (mil payloads, unos 3 MB) eso salía
// caro en los tres caminos reales:
//
//   - Len() lo llama el icono de bandeja cada 5 segundos vía agent.Status(),
//     así que el servicio leía 3 MB de disco cada 5 segundos, indefinidamente,
//     solo para enseñar un número en un menú.
//   - Drenar el buffer entero eran mil Pop, cada uno con su lectura y su
//     escritura completas: del orden de 6 GB de entrada/salida para mover
//     3 MB de datos. Coste cuadrático.
//   - Cada Push reescribía el fichero completo, una vez por heartbeat fallido.
//
// Ahora:
//
//   - Len() devuelve un contador en memoria, sin tocar disco.
//   - Push añade al final del fichero (coste constante) y solo compacta de
//     vez en cuando, ver rotateSlack.
//   - PopBatch extrae N payloads con UNA lectura y UNA escritura, en vez de N
//     de cada.
//
// # Quién puede escribir el fichero
//
// El contador en memoria asume que este proceso es el ÚNICO que escribe el
// fichero, que es el caso: lo abre el servicio y nadie más. Si alguien lo
// edita o lo borra por fuera, Len() puede quedar desfasado hasta la siguiente
// operación que lea (PopBatch resincroniza el contador con lo que haya de
// verdad en disco).
package buffer

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// RingBuffer es seguro para uso concurrente: el bucle principal del agente
// llama a Push/Pop mientras el canal de control (§11.4) puede llamar a Len
// desde otra goroutine en cualquier momento (agent.Status()). El mutex es
// quien garantiza eso, no el caller.
type RingBuffer struct {
	mu       sync.Mutex
	path     string
	maxItems int

	// count es cuántos payloads hay en el fichero. Se mantiene en memoria
	// para que Len() no tenga que leerlo entero; loaded distingue "cero
	// payloads" de "todavía no lo he mirado".
	count  int
	loaded bool
}

func NewRingBuffer(path string, maxItems int) *RingBuffer {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &RingBuffer{path: path, maxItems: maxItems}
}

// rotateSlack es cuántos payloads de más se toleran en disco antes de
// compactar.
//
// Sin holgura, un buffer lleno vuelve a reescribir el fichero entero en cada
// Push, que es justo el coste que se quería quitar: durante una caída larga
// del backend eso son 3 MB leídos y 3 MB escritos cada 15 segundos, unos
// 11 GB a lo largo de ocho horas. Con holgura se compacta una vez cada
// `rotateSlack` inserciones, así que el coste por Push queda constante en
// promedio.
//
// El precio es que el fichero puede tener hasta maxItems+rotateSlack
// payloads en un momento dado. Sigue siendo un tope duro —que es lo que el
// §5 exige— solo que un 10 % más alto que el nominal.
func (b *RingBuffer) rotateSlack() int {
	slack := b.maxItems / 10
	switch {
	case slack < 1:
		return 1
	case slack > 256:
		return 256
	default:
		return slack
	}
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
	defer func() { _ = f.Close() }()
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
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
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

// appendLine añade una línea al final del fichero sin leer lo que ya hay.
func appendLine(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// Una sola escritura, no dos: con O_APPEND, dos escrituras podrían
	// intercalarse con las de otro proceso y partir la línea.
	line := make([]byte, 0, len(data)+1)
	line = append(line, data...)
	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ensureLoadedLocked establece el contador la primera vez, leyendo el fichero
// una única vez en la vida del proceso. Requiere b.mu tomado.
func (b *RingBuffer) ensureLoadedLocked() error {
	if b.loaded {
		return nil
	}
	lines, err := readLines(b.path)
	if err != nil {
		return err
	}
	b.count = len(lines)
	b.loaded = true
	return nil
}

// Push encola un payload. Si el buffer está lleno, descarta el más viejo.
func (b *RingBuffer) Push(p *payload.Payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLoadedLocked(); err != nil {
		return err
	}
	if err := appendLine(b.path, data); err != nil {
		return err
	}
	b.count++

	if b.count > b.maxItems+b.rotateSlack() {
		return b.compactLocked()
	}
	return nil
}

// compactLocked reescribe el fichero quedándose con los maxItems más
// recientes, descartando los más viejos (§5). Requiere b.mu tomado.
func (b *RingBuffer) compactLocked() error {
	lines, err := readLines(b.path)
	if err != nil {
		return err
	}
	if len(lines) > b.maxItems {
		lines = lines[len(lines)-b.maxItems:]
	}
	if err := writeLines(b.path, lines); err != nil {
		return err
	}
	b.count = len(lines)
	return nil
}

// Pop devuelve el payload más viejo y lo elimina. io.EOF = buffer vacío.
func (b *RingBuffer) Pop() (*payload.Payload, error) {
	ps, err := b.PopBatch(1)
	if err != nil {
		return nil, err
	}
	return ps[0], nil
}

// PopBatch extrae hasta n payloads del principio de la cola, de más viejo a
// más reciente, con una sola lectura y una sola escritura del fichero.
//
// Existe por el drenado: enviarlos de uno en uno con Pop hacía que vaciar el
// buffer costase N lecturas y N escrituras completas del fichero, un coste
// cuadrático en el número de payloads pendientes.
//
// Devuelve io.EOF si no hay nada que extraer. Las líneas corruptas se
// descartan por el camino; si TODAS las que se consumieron lo estaban, se
// devuelve el error de decodificación (pero las líneas ya no están, así que
// la llamada siguiente avanza en vez de tropezar con las mismas).
func (b *RingBuffer) PopBatch(n int) ([]*payload.Payload, error) {
	if n <= 0 {
		return nil, io.EOF
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	lines, err := readLines(b.path)
	if err != nil {
		return nil, err
	}
	// Resincronizar: si alguien tocó el fichero por fuera, esta es la
	// oportunidad de enterarse.
	b.count = len(lines)
	b.loaded = true

	if len(lines) == 0 {
		return nil, io.EOF
	}
	if n > len(lines) {
		n = len(lines)
	}

	out := make([]*payload.Payload, 0, n)
	var decodeErr error
	for _, line := range lines[:n] {
		var p payload.Payload
		if err := json.Unmarshal(line, &p); err != nil {
			// Línea corrupta (fichero manipulado, escritura a medias tras un
			// corte de luz): se descarta igualmente para no quedar trabados
			// leyéndola una y otra vez.
			decodeErr = err
			continue
		}
		out = append(out, &p)
	}

	if err := writeLines(b.path, lines[n:]); err != nil {
		return nil, err
	}
	b.count = len(lines) - n

	if len(out) == 0 {
		// Todo lo consumido estaba corrupto: se reporta, ya sin esas líneas.
		if decodeErr != nil {
			return nil, decodeErr
		}
		return nil, io.EOF
	}
	return out, nil
}

// Len devuelve el número de payloads pendientes.
//
// Sale del contador en memoria, sin tocar disco: lo llama agent.Status(), y
// el icono de bandeja pregunta el estado cada 5 segundos. Leyendo el fichero
// entero, eso eran megabytes de disco cada pocos segundos para siempre.
func (b *RingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLoadedLocked(); err != nil {
		return 0
	}
	return b.count
}
