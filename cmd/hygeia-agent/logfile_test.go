package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// El fichero de log se abría en modo añadir y no se rotaba NUNCA: a un
// heartbeat cada 15 s eran unos 350 MB al año por equipo, contradiciendo el
// principio del §5 de que nada crece sin límite.
func TestRotatingWriterKeepsTheFileBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hygeia-agent.log")

	const maxBytes = 512
	w, err := newRotatingWriter(path, maxBytes)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	line := []byte(strings.Repeat("x", 100) + "\n")
	for i := 0; i < 100; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if st.Size() > maxBytes {
			t.Fatalf("el fichero llegó a %d bytes, el tope es %d", st.Size(), maxBytes)
		}
	}

	// Se conserva exactamente un fichero anterior, ni cero ni un histórico.
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("no se conservó el fichero anterior: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Error("apareció un .2: solo debe conservarse un fichero anterior")
	}
}

// Tras un reinicio del servicio, la cuenta de tamaño tiene que partir de lo
// que el fichero ya ocupa. Si empezara de cero, el fichero podría crecer sin
// tope a base de reinicios.
func TestRotatingWriterResumesFromExistingSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hygeia-agent.log")

	if err := os.WriteFile(path, []byte(strings.Repeat("y", 400)), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := newRotatingWriter(path, 512)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	// 400 que ya había + 200 pasa del tope: debe rotar, no acumular 600.
	if _, err := w.Write([]byte(strings.Repeat("z", 200))); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > 512 {
		t.Errorf("el fichero quedó en %d bytes: no se tuvo en cuenta el tamaño previo", st.Size())
	}
}
