package logring

import (
	"reflect"
	"sync"
	"testing"
)

func TestWriteSplitsAndRetainsLines(t *testing.T) {
	b := New(10)
	b.Write([]byte("línea uno\nlínea dos\n"))
	b.Write([]byte("línea tres\n"))

	got := b.Lines()
	want := []string{"línea uno", "línea dos", "línea tres"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lines() = %v, se esperaba %v", got, want)
	}
}

func TestWritePartialLineWithoutNewline(t *testing.T) {
	b := New(10)
	b.Write([]byte("sin salto de línea todavía"))
	if got := b.Lines(); len(got) != 0 {
		t.Errorf("Lines() = %v, se esperaba vacío (línea sin terminar)", got)
	}

	b.Write([]byte(" -- ahora sí\n"))
	got := b.Lines()
	want := []string{"sin salto de línea todavía -- ahora sí"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lines() = %v, se esperaba %v", got, want)
	}
}

// Acotado: al superar la capacidad, se descartan las líneas más viejas —
// mismo principio que el ring buffer en disco (§5).
func TestEvictsOldestBeyondCapacity(t *testing.T) {
	b := New(3)
	for _, l := range []string{"a", "b", "c", "d"} {
		b.Write([]byte(l + "\n"))
	}
	want := []string{"b", "c", "d"}
	if got := b.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("Lines() = %v, se esperaba %v", got, want)
	}
}

func TestLinesReturnsACopy(t *testing.T) {
	b := New(10)
	b.Write([]byte("a\n"))
	got := b.Lines()
	got[0] = "mutado"
	if fresh := b.Lines(); fresh[0] != "a" {
		t.Errorf("mutar el slice devuelto afectó al buffer interno: %v", fresh)
	}
}

// Buffer implementa io.Writer y se conecta en paralelo al logger real —
// debe ser seguro para escrituras concurrentes.
func TestConcurrentWrites(t *testing.T) {
	b := New(1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Write([]byte("x\n"))
		}()
	}
	wg.Wait()
	if got := len(b.Lines()); got != 50 {
		t.Errorf("Lines() tiene %d líneas, se esperaban 50", got)
	}
}
