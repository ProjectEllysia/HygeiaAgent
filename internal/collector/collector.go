package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Collector es la interfaz común a todas las familias de métricas.
// Cada implementación rellena SU campo dentro de *payload.Metrics.
type Collector interface {
	Name() string
	Collect(ctx context.Context, m *payload.Metrics) error
}

// Registry mapea nombres de config (["cpu","memory",...]) a factories.
//
// order guarda los nombres en el orden en que se registraron: un mapa de Go
// no tiene orden propio, y Build necesita uno estable para derivar de aquí
// la lista de colectores por defecto en vez de repetirla como literal aparte
// (que es justo el fallo que tenía este fichero: la lista de NewRegistry y
// la de Build podían divergir sin que nada lo impidiera).
type Registry struct {
	factories map[string]func() Collector
	order     []string
}

func NewRegistry() *Registry {
	r := &Registry{factories: make(map[string]func() Collector)}
	r.Register("cpu", NewCPU)
	r.Register("memory", NewMemory)
	r.Register("disk", NewDisk)
	r.Register("network", NewNetwork)
	r.Register("processes", NewProcesses)
	r.Register("power", NewPower)
	return r
}

func (r *Registry) Register(name string, factory func() Collector) {
	if _, exists := r.factories[name]; !exists {
		r.order = append(r.order, name)
	}
	r.factories[name] = factory
}

// Build construye los colectores pedidos; si names es vacío usa TODOS los
// registrados, en su orden de alta. Los nombres desconocidos se ignoran (el
// backend descarta lo desconocido, pero aquí no tiene sentido fallar el
// arranque por un typo).
func (r *Registry) Build(names []string) []Collector {
	if len(names) == 0 {
		names = r.order
	}
	var cs []Collector
	for _, n := range names {
		if f, ok := r.factories[n]; ok {
			cs = append(cs, f())
		}
	}
	return cs
}
