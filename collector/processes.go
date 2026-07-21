package collector

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	gpsproc "github.com/shirou/gopsutil/v4/process"
)

// topN es el número de procesos que se reportan en topCpu/topMem (§3).
const topN = 5

// ProcessCollector guarda el tiempo de CPU acumulado de cada PID entre
// ciclos (mismo patrón que NetworkCollector con las interfaces) para poder
// calcular una TASA reciente de uso de CPU, en vez de la media histórica
// que da gopsutil de fábrica (ver comentario de Collect, plan §12.1).
type ProcessCollector struct {
	mu       sync.Mutex
	prevCPU  map[int32]float64 // pid -> segundos de CPU totales, ciclo anterior
	prevTime time.Time
}

func NewProcesses() Collector {
	return &ProcessCollector{prevCPU: make(map[int32]float64)}
}

func (c *ProcessCollector) Name() string { return "processes" }

type procSample struct {
	pid  int32
	proc *gpsproc.Process
	cpu  float64
	mem  float64
}

// Collect recorre todos los procesos y arma el top-N por CPU y por memoria
// (§3).
//
// El % de CPU se calcula como delta de tiempo de CPU entre este ciclo y el
// anterior dividido por el tiempo transcurrido — el mismo patrón que
// network.go ya usa para las interfaces — en vez de
// Process.CPUPercentWithContext() de gopsutil, que divide el tiempo de CPU
// TOTAL entre el tiempo transcurrido DESDE QUE EL PROCESO ARRANCÓ: es una
// media de toda la vida del proceso, no el uso reciente. Un proceso de días
// que empieza a picar al 100 % ahora mismo saldría con un cpuPct casi cero
// bajo ese cálculo — justo el escenario que el §3 pone como ejemplo de por
// qué existe el top-N (plan §12.1).
//
// MemoryPercent no tiene ese problema (RSS actual / total, instantáneo), así
// que topMem no necesita este tratamiento.
func (c *ProcessCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	procs, err := gpsproc.ProcessesWithContext(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	c.mu.Lock()
	prevCPU := c.prevCPU
	prevTime := c.prevTime
	c.mu.Unlock()

	elapsed := now.Sub(prevTime).Seconds()
	// El primer ciclo (o uno tras un hueco extraño de reloj) no tiene
	// referencia: todas las tasas de CPU serían 0 y el "top" sería
	// arbitrario, así que se omite el top-N por CPU ese ciclo — igual que
	// network.go omite las interfaces nuevas.
	haveBaseline := elapsed > 0 && len(prevCPU) > 0

	newCPU := make(map[int32]float64, len(procs))
	samples := make([]procSample, 0, len(procs))
	var zombie uint64
	for _, p := range procs {
		if status, err := p.StatusWithContext(ctx); err == nil {
			for _, s := range status {
				if s == gpsproc.Zombie {
					zombie++
				}
			}
		}

		var cpuPct float64
		if cput, err := p.TimesWithContext(ctx); err == nil {
			total := cput.Total()
			newCPU[p.Pid] = total
			if haveBaseline {
				if prev, ok := prevCPU[p.Pid]; ok && total >= prev {
					cpuPct = 100 * (total - prev) / elapsed
				}
			}
		}

		mem, _ := p.MemoryPercentWithContext(ctx)
		samples = append(samples, procSample{pid: p.Pid, proc: p, cpu: cpuPct, mem: float64(mem)})
	}

	c.mu.Lock()
	c.prevCPU = newCPU
	c.prevTime = now
	c.mu.Unlock()

	var topCPU []payload.ProcessInfo
	if haveBaseline {
		sort.Slice(samples, func(i, j int) bool { return samples[i].cpu > samples[j].cpu })
		topCPU = toProcessInfo(ctx, samples, true)
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].mem > samples[j].mem })
	topMem := toProcessInfo(ctx, samples, false)

	m.Processes = &payload.ProcessMetrics{
		Total:  uint64(len(procs)),
		Zombie: zombie,
		TopCPU: topCPU,
		TopMem: topMem,
	}
	return nil
}

// toProcessInfo lee el nombre solo de los N procesos que ya se sabe que
// entran en el top, reusando el *process.Process obtenido en Collect en vez
// de recrearlo por PID (evita una revalidación de existencia redundante;
// plan §12.2, Tier 2).
func toProcessInfo(ctx context.Context, samples []procSample, byCPU bool) []payload.ProcessInfo {
	n := topN
	if len(samples) < n {
		n = len(samples)
	}
	out := make([]payload.ProcessInfo, 0, n)
	for i := 0; i < n; i++ {
		s := samples[i]
		name, _ := s.proc.NameWithContext(ctx)
		pi := payload.ProcessInfo{PID: s.pid, Name: name}
		if byCPU {
			pi.CPUPct = round1(s.cpu)
		} else {
			pi.MemPct = round1(s.mem)
		}
		out = append(out, pi)
	}
	return out
}
