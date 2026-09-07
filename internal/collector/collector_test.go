package collector

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

type fakeCollector struct{ name string }

func (f *fakeCollector) Name() string                                    { return f.name }
func (f *fakeCollector) Collect(context.Context, *payload.Metrics) error { return nil }

// discardLogger evita ensuciar la salida de los tests con los avisos que
// pueda emitir el collector "power" al no encontrar fuente.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegistryBuildDefaultsToAllWhenNamesEmpty(t *testing.T) {
	r := NewRegistry(discardLogger())
	// Comparamos contra r.order y no contra una lista literal: si este test
	// tuviera su propia copia de los nombres, sería la TERCERA copia de la
	// lista, exactamente el fallo que este cambio elimina.
	got := namesOf(r.Build(nil))
	want := append([]string(nil), r.order...)
	if len(got) != len(want) {
		t.Fatalf("Build(nil) = %v, se esperaba un colector por cada nombre registrado (%v)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Build(nil)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistryBuildDefaultIncludesPower(t *testing.T) {
	r := NewRegistry(discardLogger())
	found := false
	for _, c := range r.Build(nil) {
		if c.Name() == "power" {
			found = true
		}
	}
	if !found {
		t.Error("Build(nil) no incluyó el collector \"power\": debe estar activo por defecto")
	}
}

func TestRegistryBuildFiltersUnknownNames(t *testing.T) {
	r := NewRegistry(discardLogger())
	cs := r.Build([]string{"cpu", "un-typo-que-no-existe", "memory"})
	if got := len(cs); got != 2 {
		t.Fatalf("Build con un nombre desconocido devolvió %d, se esperaban 2 (cpu, memory)", got)
	}
	if cs[0].Name() != "cpu" || cs[1].Name() != "memory" {
		t.Errorf("Build = %v, se esperaba [cpu memory] en ese orden", namesOf(cs))
	}
}

func TestRegistryBuildRespectsRequestedOrder(t *testing.T) {
	r := &Registry{factories: map[string]func() Collector{}}
	r.Register("a", func() Collector { return &fakeCollector{name: "a"} })
	r.Register("b", func() Collector { return &fakeCollector{name: "b"} })

	cs := r.Build([]string{"b", "a"})
	if got := namesOf(cs); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("Build([\"b\",\"a\"]) = %v, se esperaba [b a]", got)
	}
}

func namesOf(cs []Collector) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name()
	}
	return out
}
