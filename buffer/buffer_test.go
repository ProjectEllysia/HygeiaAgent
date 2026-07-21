package buffer

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

// Al llenarse, el buffer descarta el ítem más viejo (§5: acotado, nunca
// crece sin límite).
func TestPushEvictsOldestWhenFull(t *testing.T) {
	b := NewRingBuffer(filepath.Join(t.TempDir(), "buffer.jsonl"), 3)
	for _, tag := range []string{"a", "b", "c", "d"} {
		if err := b.Push(newTestPayload(tag)); err != nil {
			t.Fatalf("Push(%s): %v", tag, err)
		}
	}
	if got := b.Len(); got != 3 {
		t.Fatalf("Len() = %d, se esperaba 3 (acotado)", got)
	}
	p, err := b.Pop()
	if err != nil {
		t.Fatalf("Pop(): %v", err)
	}
	if p.AgentVersion != "b" {
		t.Errorf("Pop() tras eviction = %q, se esperaba \"b\" (\"a\" fue descartado)", p.AgentVersion)
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
