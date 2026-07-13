package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestNetworkCollector_Name(t *testing.T) {
	c := NewNetwork()
	if got := c.Name(); got != "network" {
		t.Errorf("Name() = %q, want %q", got, "network")
	}
}

func TestNetworkCollector_Collect(t *testing.T) {
	c := NewNetwork()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if len(m.Network) != 0 {
		t.Errorf("expected empty Network, got %v", m.Network)
	}
}
