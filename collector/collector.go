package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

// Collector es la interfaz común a todas las familias de métricas.
// Cada implementación rellena SU campo dentro de *payload.Metrics.
type Collector interface {
	Name() string
	Collect(ctx context.Context, m *payload.Metrics) error
}

// Registry mapea nombres de config (["cpu","memory",...]) a factories.
type Registry struct {
	factories map[string]func() Collector
}

func NewRegistry() *Registry {
	r := &Registry{factories: make(map[string]func() Collector)}
	r.Register("cpu", NewCPU)
	r.Register("memory", NewMemory)
	r.Register("disk", NewDisk)
	r.Register("network", NewNetwork)
	r.Register("processes", NewProcesses)
	return r
}

func (r *Registry) Register(name string, factory func() Collector) {
	r.factories[name] = factory
}

// Build construye los colectores pedidos; si names es vacío usa todos.
// Los nombres desconocidos se ignoran (el backend descarta lo desconocido,
// pero aquí no tiene sentido fallar el arranque por un typo).
func (r *Registry) Build(names []string) []Collector {
	if len(names) == 0 {
		names = []string{"cpu", "memory", "disk", "network", "processes"}
	}
	var cs []Collector
	for _, n := range names {
		if f, ok := r.factories[n]; ok {
			cs = append(cs, f())
		}
	}
	return cs
}
