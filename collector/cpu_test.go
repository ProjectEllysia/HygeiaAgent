package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestCPUCollector_Name(t *testing.T) {
	c := NewCPU()
	if got := c.Name(); got != "cpu" {
		t.Errorf("Name() = %q, want %q", got, "cpu")
	}
}

func TestCPUCollector_Collect(t *testing.T) {
	c := NewCPU()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.CPU != nil {
		t.Errorf("expected CPU to be nil (not yet implemented)")
	}
}
