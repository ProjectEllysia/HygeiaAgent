package buffer

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func newTestPayload(tag string) *payload.Payload {
	return &payload.Payload{AgentVersion: tag}
}

func TestPushPopFIFO(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 10)

	if err := b.Push(newTestPayload("a")); err != nil {
		t.Fatalf("Push(a): %v", err)
	}
	if err := b.Push(newTestPayload("b")); err != nil {
		t.Fatalf("Push(b): %v", err)
	}
	if got := b.Len(); got != 2 {
		t.Fatalf("Len() = %d, se esperaba 2", got)
	}

	p, err := b.Pop()
	if err != nil {
		t.Fatalf("Pop(): %v", err)
	}
	if p.AgentVersion != "a" {
		t.Errorf("Pop() = %q, se esperaba \"a\" (FIFO)", p.AgentVersion)
	}
	if got := b.Len(); got != 1 {
		t.Errorf("Len() tras un Pop = %d, se esperaba 1", got)
	}
}

func TestPopEmptyReturnsEOF(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 10)
	if _, err := b.Pop(); err != io.EOF {
		t.Errorf("Pop() en buffer vacío = %v, se esperaba io.EOF", err)
	}
}

// Al llenarse, el buffer descarta lo más viejo (§5: acotado, nunca crece sin
// límite).
//
// El tope efectivo es maxItems+rotateSlack, no maxItems exacto: compactar en
// cada Push una vez lleno significaba reescribir el fichero entero cada 15 s
// durante toda una caída del backend, que es justo el coste que rotateSlack
// existe para amortizar. Lo que sí se mantiene intacto es la garantía que
// importa: el crecimiento está acotado y lo que se descarta es siempre lo más
// viejo.
func TestPushStaysBoundedAndEvictsOldest(t *testing.T) {
	const maxItems = 3
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), maxItems)
	ceiling := maxItems + b.rotateSlack()

	// Suficientes inserciones para forzar varias compactaciones.
	for i := 0; i < 50; i++ {
		if err := b.Push(newTestPayload(strconv.Itoa(i))); err != nil {
			t.Fatalf("Push(%d): %v", i, err)
		}
		if got := b.Len(); got > ceiling {
			t.Fatalf("Len() = %d tras %d inserciones, supera el tope de %d", got, i+1, ceiling)
		}
	}

	// Lo que queda son los más RECIENTES: el más viejo que sobreviva nunca
	// puede ser el 0, y la secuencia debe salir en orden y sin huecos.
	got, err := b.PopBatch(ceiling)
	if err != nil {
		t.Fatalf("PopBatch(): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("PopBatch() no devolvió nada")
	}
	first, err := strconv.Atoi(got[0].AgentVersion)
	if err != nil {
		t.Fatalf("etiqueta inesperada %q", got[0].AgentVersion)
	}
	if first == 0 {
		t.Error("el payload más viejo sigue siendo el primero: no se descartó nada")
	}
	for i, p := range got {
		if want := strconv.Itoa(first + i); p.AgentVersion != want {
			t.Errorf("got[%d] = %q, se esperaba %q (orden FIFO sin huecos)", i, p.AgentVersion, want)
		}
	}
}

// PopBatch es la razón de ser de A-07: extraer N payloads con UNA lectura y
// UNA escritura, en vez de N de cada.
func TestPopBatchReturnsOldestFirst(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 100)
	for _, tag := range []string{"a", "b", "c", "d", "e"} {
		if err := b.Push(newTestPayload(tag)); err != nil {
			t.Fatalf("Push(%s): %v", tag, err)
		}
	}

	got, err := b.PopBatch(3)
	if err != nil {
		t.Fatalf("PopBatch(3): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(PopBatch(3)) = %d, se esperaba 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].AgentVersion != want {
			t.Errorf("got[%d] = %q, se esperaba %q", i, got[i].AgentVersion, want)
		}
	}
	if n := b.Len(); n != 2 {
		t.Errorf("Len() tras PopBatch(3) = %d, se esperaba 2", n)
	}
}

// Pedir más de lo que hay devuelve lo que hay, no un error.
func TestPopBatchClampsToWhatIsAvailable(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 100)
	if err := b.Push(newTestPayload("solo-uno")); err != nil {
		t.Fatal(err)
	}

	got, err := b.PopBatch(10)
	if err != nil {
		t.Fatalf("PopBatch(10): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, se esperaba 1", len(got))
	}
	if _, err := b.PopBatch(10); err != io.EOF {
		t.Errorf("PopBatch() sobre buffer vacío = %v, se esperaba io.EOF", err)
	}
}

// Len() sale de un contador en memoria, así que tiene que sobrevivir a un
// reinicio del proceso: un RingBuffer nuevo sobre un fichero que ya existe
// debe contar lo que hay, no partir de cero.
func TestLenReflectsAnExistingFileOnStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.jsonl")

	first := NewRingBuffer(path, 100)
	for i := 0; i < 7; i++ {
		if err := first.Push(newTestPayload("x")); err != nil {
			t.Fatal(err)
		}
	}

	// Otro RingBuffer sobre el mismo fichero: simula el reinicio del servicio.
	second := NewRingBuffer(path, 100)
	if got := second.Len(); got != 7 {
		t.Errorf("Len() tras reiniciar = %d, se esperaba 7", got)
	}
}

// Una línea corrupta (fichero manipulado externamente, escritura a medias
// tras un corte de luz) no debe dejar el buffer trabado para siempre: Pop la
// descarta y reporta el error, dejando el resto del buffer intacto y
// accesible en la siguiente llamada.
func TestPopRecoversFromCorruptedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer.jsonl")
	b := NewRingBuffer(path, 10)

	good, err := json.Marshal(newTestPayload("valida"))
	if err != nil {
		t.Fatal(err)
	}
	// Fichero escrito a mano: una línea corrupta seguida de una válida.
	content := "{esto no es json valido\n" + string(good) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := b.Pop(); err == nil {
		t.Fatal("Pop() sobre la línea corrupta = nil, se esperaba un error de decodificación")
	}

	p, err := b.Pop()
	if err != nil {
		t.Fatalf("Pop() tras descartar la corrupta: %v", err)
	}
	if p.AgentVersion != "valida" {
		t.Errorf("Pop() = %q, se esperaba \"valida\" (la línea buena tras la corrupta)", p.AgentVersion)
	}
	if _, err := b.Pop(); err != io.EOF {
		t.Errorf("Pop() tras vaciar = %v, se esperaba io.EOF", err)
	}
}

// Un fichero de buffer que no existe todavía (primer arranque del agente)
// no es un error: el buffer se comporta como si estuviera vacío.
func TestBufferToleratesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-existe-todavia.jsonl")
	b := NewRingBuffer(path, 10)

	if got := b.Len(); got != 0 {
		t.Errorf("Len() sobre fichero inexistente = %d, se esperaba 0", got)
	}
	if _, err := b.Pop(); err != io.EOF {
		t.Errorf("Pop() sobre fichero inexistente = %v, se esperaba io.EOF", err)
	}
}

// Push/Pop/Len deben ser seguros para uso concurrente: el bucle principal
// del agente llama a Push/Pop mientras el canal de control puede llamar a
// Len desde otra goroutine en cualquier momento (agent.Status()). Sin el
// mutex interno, este test corrompía el fichero o perdía escrituras.
func TestConcurrentAccess(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 1000)

	const writers = 20
	const perWriter = 25

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if err := b.Push(newTestPayload("x")); err != nil {
					t.Errorf("Push concurrente: %v", err)
				}
			}
		}()
	}

	// Un lector concurrente machaca Len() mientras los escritores corren —
	// esto es exactamente el patrón real (Status() vs runOnce/drainBuffer).
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-stop:
				return
			default:
				b.Len()
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-finished

	if got := b.Len(); got != writers*perWriter {
		t.Errorf("Len() tras la ráfaga concurrente = %d, se esperaba %d", got, writers*perWriter)
	}
}

// Referencias de coste de los dos caminos que A-07 arregla.

// Len() lo llama agent.Status(), y el icono de bandeja pregunta el estado
// cada 5 segundos. Leyendo el fichero entero, un buffer lleno significaba
// megabytes de disco cada pocos segundos, indefinidamente.
func BenchmarkLenOnFullBuffer(b *testing.B) {
	buf := newBenchBuffer(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buf.Len()
	}
}

// Drenar el buffer entero: el camino cuadrático. Cada iteración vacía mil
// payloads y vuelve a llenarlos.
func BenchmarkDrainFullBuffer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		buf := newBenchBuffer(b, 1000)
		b.StartTimer()
		for {
			if _, err := buf.PopBatch(5); err != nil {
				break
			}
		}
	}
}

func newBenchBuffer(tb testing.TB, n int) *RingBuffer {
	tb.Helper()
	buf := NewRingBuffer(filepath.Join(tb.TempDir(), "buffer.jsonl"), n)
	for i := 0; i < n; i++ {
		if err := buf.Push(newTestPayload("bench")); err != nil {
			tb.Fatal(err)
		}
	}
	return buf
}
