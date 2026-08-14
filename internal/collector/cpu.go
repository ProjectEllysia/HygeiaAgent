package collector

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	gpscpu "github.com/shirou/gopsutil/v4/cpu"
	gpsload "github.com/shirou/gopsutil/v4/load"
)

// CPUCollector guarda los contadores de tiempo de CPU del ciclo anterior
// para calcular el uso como diferencia entre ciclos — el mismo patrón que ya
// usaban network.go y processes.go.
type CPUCollector struct {
	mu   sync.Mutex
	prev []gpscpu.TimesStat
}

func NewCPU() Collector { return &CPUCollector{} }

func (c *CPUCollector) Name() string { return "cpu" }

// Collect mide el uso de CPU como diferencia de los contadores del sistema
// entre este ciclo y el anterior.
//
// Antes se usaba gopsutil Percent(interval, percpu), que toma una muestra,
// DUERME el intervalo indicado y toma otra: un segundo de bloqueo real en
// cada ciclo. Con el intervalo por defecto de 15 s, el agente pasaba una de
// cada quince unidades de tiempo parado ahí, y ese segundo salía además de la
// cuota de 5 s que collectPayload concede a cada colector.
//
// Guardando la muestra anterior no hace falta dormir: los contadores del
// sistema ya son acumulados, así que la diferencia entre dos ciclos consecutivos
// da el uso del intervalo completo. Es además un dato MEJOR para lo que el
// backend hace con él: sus umbrales piden carga sostenida
// (sustainedHeartbeats), no un pico de un segundo, y una media sobre los 15 s
// enteros describe eso con más fidelidad que una foto de un segundo.
//
// El primer ciclo no tiene con qué comparar y ahí sí se usa el muestreo
// bloqueante — una vez en la vida del proceso, no una por ciclo. Tiene que
// haber dato desde el principio porque metrics.cpu es obligatorio en el
// esquema de ingesta: un heartbeat sin él se rechaza entero.
func (c *CPUCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	times, err := gpscpu.TimesWithContext(ctx, true)
	if err != nil {
		return err
	}

	c.mu.Lock()
	prev := c.prev
	c.prev = times
	c.mu.Unlock()

	perCore, err := c.perCorePct(ctx, prev, times)
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

// perCorePct devuelve el uso por núcleo. Con muestra anterior utilizable, es
// la diferencia entre ambas; sin ella (primer ciclo, o el número de núcleos
// cambió por un hot-plug) cae al muestreo bloqueante, que es la única forma
// de tener un dato en ese momento.
func (c *CPUCollector) perCorePct(ctx context.Context, prev, cur []gpscpu.TimesStat) ([]float64, error) {
	if len(prev) == 0 || len(prev) != len(cur) {
		return gpscpu.PercentWithContext(ctx, sampleInterval(), true)
	}

	out := make([]float64, len(cur))
	for i := range cur {
		out[i] = roundPct(deltaPct(prev[i], cur[i]))
	}
	return out, nil
}

// deltaPct calcula el porcentaje de tiempo ocupado de un núcleo entre dos
// lecturas de sus contadores acumulados.
//
// El reparto de qué cuenta como ocupado replica el de gopsutil (getAllBusy):
// en Linux, Guest y GuestNice ya vienen sumados dentro de User y Nice, así
// que contarlos otra vez inflaría el total; e Idle e Iowait son las dos
// formas de no estar haciendo trabajo.
func deltaPct(prev, cur gpscpu.TimesStat) float64 {
	prevTotal, prevBusy := busy(prev)
	curTotal, curBusy := busy(cur)

	deltaTotal := curTotal - prevTotal
	if deltaTotal <= 0 {
		// Contadores sin avanzar o que dieron la vuelta (suspensión, migración
		// de la máquina virtual): no hay nada que medir en este intervalo.
		return 0
	}
	return 100 * (curBusy - prevBusy) / deltaTotal
}

func busy(t gpscpu.TimesStat) (total, busy float64) {
	total = cpuTotal(t)
	if runtime.GOOS == "linux" {
		total -= t.Guest
		total -= t.GuestNice
	}
	return total, total - t.Idle - t.Iowait
}

// cpuTotal suma todos los contadores de tiempo de un núcleo.
//
// Sustituye a gpscpu.TimesStat.Total(), que gopsutil marcó como deprecated
// por tratarse de un detalle interno suyo. Sumarlo aquí, además de quitar la
// dependencia de una API que puede desaparecer, deja explícito qué entra en
// el total — que es justo lo que busy() necesita saber para restar después.
func cpuTotal(t gpscpu.TimesStat) float64 {
	return t.User + t.System + t.Idle + t.Nice + t.Iowait +
		t.Irq + t.Softirq + t.Steal + t.Guest + t.GuestNice
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
