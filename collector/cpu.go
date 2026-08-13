package collector

import (
	"context"
	"math"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	gpscpu "github.com/shirou/gopsutil/v4/cpu"
	gpsload "github.com/shirou/gopsutil/v4/load"
)

type CPUCollector struct{}

func NewCPU() Collector { return &CPUCollector{} }

func (c *CPUCollector) Name() string { return "cpu" }

// Collect mide el uso de CPU. gopsutil/v4 expone Percent(interval, percpu)
// que bloquea `interval` muestreando los contadores del SO: es la forma
// fiable de obtener un % instantáneo cross-platform. Como mucho 1s.
func (c *CPUCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	perCore, err := gpscpu.PercentWithContext(ctx, sampleInterval(), true)
	if err != nil {
		return err
	}

	var global float64
	if len(perCore) > 0 {
		var sum float64
		for _, v := range perCore {
			sum += v
		}
		global = sum / float64(len(perCore))
	}

	out := &payload.CPUMetrics{
		UsagePct:   roundPct(global),
		PerCorePct: perCore,
	}

	// Load average solo existe en UNIX; en Windows Avg() devuelve error y lo
	// omitimos (README §3).
	if avg, err := gpsload.Avg(); err == nil {
		out.LoadAvg = []float64{avg.Load1, avg.Load5, avg.Load15}
	}

	m.CPU = out
	return nil
}

func sampleInterval() time.Duration { return time.Second }

// roundPct redondea un porcentaje a 1 decimal y lo ACOTA a [0, 100].
//
// El recorte no es cosmético. El schema de ingesta del backend valida cada
// porcentaje con Range(min=0, max=100) y, si uno solo se sale, rechaza el
// heartbeat ENTERO con un error de validación (422) que el shipper clasifica
// como permanente — o sea: el ciclo completo se descarta, no se guarda en el
// buffer, y el activo se queda mudo. Un salto de reloj, una hibernación o un
// contador que da la vuelta pueden producir un valor fuera de rango aunque la
// fórmula que lo calcula sea correcta, así que se corta aquí, en el único
// punto por el que pasan TODOS los porcentajes del payload (CPU, memoria,
// swap, disco y procesos).
//
// NaN se devuelve como 0 y ±Inf cae en los cortes de rango: el cast a int64
// de un valor no finito es indefinido en Go (plan §12.2, Tier 2).
func roundPct(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return float64(int64(v*10+0.5)) / 10
}
