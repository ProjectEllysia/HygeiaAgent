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

// Esta prueba antes esperaba m.Memory == nil ("not yet implemented"); quedó
// obsoleta al implementar el colector de verdad vía gopsutil y el merge con
// la rama de colectores la arrastró sin actualizar.
func TestMemoryCollector_Collect(t *testing.T) {
	c := NewMemory()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Memory == nil {
		t.Fatal("m.Memory = nil, se esperaba que el colector lo rellenara")
	}
	if m.Memory.TotalBytes == 0 {
		t.Error("TotalBytes = 0, se esperaba > 0 en cualquier host real")
	}
	if m.Memory.UsagePct < 0 || m.Memory.UsagePct > 100 {
		t.Errorf("UsagePct = %v, fuera de rango [0,100]", m.Memory.UsagePct)
	}
}
