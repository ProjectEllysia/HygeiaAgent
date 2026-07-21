package buffer

import (
	"io"
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
