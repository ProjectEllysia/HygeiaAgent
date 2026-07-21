package collector

import (
	"context"
	"math"
	"time"

	gpscpu "github.com/shirou/gopsutil/v4/cpu"
	gpsload "github.com/shirou/gopsutil/v4/load"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
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
		UsagePct:   round1(global),
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

// round1 redondea a 1 decimal. NaN/±Inf se devuelven como 0: el cast a
// int64 de un valor no finito es indefinido en Go, y ningún colector
// debería propagar un dato así al payload (plan §12.2, Tier 2).
func round1(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return float64(int64(v*10+0.5)) / 10
}