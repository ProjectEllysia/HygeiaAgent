package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

type fakeCollector struct{ name string }

func (f *fakeCollector) Name() string                                    { return f.name }
func (f *fakeCollector) Collect(context.Context, *payload.Metrics) error { return nil }

func TestRegistryBuildDefaultsToAllWhenNamesEmpty(t *testing.T) {
	r := NewRegistry()
	if got := len(r.Build(nil)); got != 5 {
		t.Errorf("Build(nil) devolvió %d colectores, se esperaban 5 (todos los registrados por defecto)", got)
	}
}

func TestRegistryBuildFiltersUnknownNames(t *testing.T) {
	r := NewRegistry()
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
