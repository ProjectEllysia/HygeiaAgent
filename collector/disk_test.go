package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestDiskCollector_Name(t *testing.T) {
	c := NewDisk()
	if got := c.Name(); got != "disk" {
		t.Errorf("Name() = %q, want %q", got, "disk")
	}
}

func TestDiskCollector_Collect(t *testing.T) {
	c := NewDisk()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if len(m.Disk) != 0 {
		t.Errorf("expected empty Disk, got %v", m.Disk)
	}
}
