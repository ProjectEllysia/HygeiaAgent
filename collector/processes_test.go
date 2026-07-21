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

// Esta prueba antes esperaba m.Processes == nil ("not yet implemented");
// quedó obsoleta al implementar el colector de verdad vía gopsutil y el
// merge con la rama de colectores la arrastró sin actualizar.
//
// topCpu/topMem se dejan sin comprobar aquí a propósito: el primer ciclo de
// ProcessCollector no tiene línea base de CPU todavía (§12.1 del plan) y
// omite topCpu deliberadamente, así que no hay un valor único "correcto"
// que afirmar sin acoplar el test a ese detalle interno.
func TestProcessCollector_Collect(t *testing.T) {
	c := NewProcesses()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Processes == nil {
		t.Fatal("m.Processes = nil, se esperaba que el colector lo rellenara")
	}
	if m.Processes.Total == 0 {
		t.Error("Total = 0, se esperaba al menos el propio proceso de test")
	}
}
