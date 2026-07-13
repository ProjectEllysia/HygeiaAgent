package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.factories) != 5 {
		t.Errorf("len(factories) = %d, want 5", len(r.factories))
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	r.Register("custom", func() Collector {
		return &stubCollector{name: "custom"}
	})
	if _, ok := r.factories["custom"]; !ok {
		t.Error("custom factory not found after Register")
	}
}

func TestRegistry_Build_All(t *testing.T) {
	r := NewRegistry()
	cs := r.Build(nil)
	if len(cs) != 5 {
		t.Errorf("len(collectors) = %d, want 5", len(cs))
	}

	names := make(map[string]bool)
	for _, c := range cs {
		names[c.Name()] = true
	}
	for _, expected := range []string{"cpu", "memory", "disk", "network", "processes"} {
		if !names[expected] {
			t.Errorf("missing collector %q", expected)
		}
	}
}

func TestRegistry_Build_Subset(t *testing.T) {
	r := NewRegistry()
	cs := r.Build([]string{"cpu", "memory"})
	if len(cs) != 2 {
		t.Fatalf("len(collectors) = %d, want 2", len(cs))
	}
	if cs[0].Name() != "cpu" {
		t.Errorf("cs[0].Name() = %q, want %q", cs[0].Name(), "cpu")
	}
	if cs[1].Name() != "memory" {
		t.Errorf("cs[1].Name() = %q, want %q", cs[1].Name(), "memory")
	}
}

func TestRegistry_Build_UnknownNames(t *testing.T) {
	r := NewRegistry()
	cs := r.Build([]string{"cpu", "unknown", "memory"})
	if len(cs) != 2 {
		t.Errorf("len(collectors) = %d, want 2 (unknown should be skipped)", len(cs))
	}
}

func TestRegistry_Build_EmptyNames(t *testing.T) {
	r := NewRegistry()
	cs := r.Build([]string{})
	if len(cs) != 5 {
		t.Errorf("len(collectors) = %d, want 5 (empty names should use defaults)", len(cs))
	}
}

// stubCollector implements Collector for testing.
type stubCollector struct {
	name string
}

func (s *stubCollector) Name() string              { return s.name }
func (s *stubCollector) Collect(context.Context, *payload.Metrics) error {
	return nil
}
