package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestMemoryCollector_Name(t *testing.T) {
	c := NewMemory()
	if got := c.Name(); got != "memory" {
		t.Errorf("Name() = %q, want %q", got, "memory")
	}
}

func TestMemoryCollector_Collect(t *testing.T) {
	c := NewMemory()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Memory != nil {
		t.Errorf("expected Memory to be nil (not yet implemented)")
	}
}
