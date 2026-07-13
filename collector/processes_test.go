package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestProcessCollector_Name(t *testing.T) {
	c := NewProcesses()
	if got := c.Name(); got != "processes" {
		t.Errorf("Name() = %q, want %q", got, "processes")
	}
}

func TestProcessCollector_Collect(t *testing.T) {
	c := NewProcesses()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Processes != nil {
		t.Errorf("expected Processes to be nil (not yet implemented)")
	}
}
