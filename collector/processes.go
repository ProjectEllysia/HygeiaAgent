package collector

import (
	"context"
	"sort"

	gpsproc "github.com/shirou/gopsutil/v4/process"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

// topN es el número de procesos que se reportan en topCpu/topMem (§3).
const topN = 5

type ProcessCollector struct{}

func NewProcesses() Collector { return &ProcessCollector{} }

func (c *ProcessCollector) Name() string { return "processes" }

type procSample struct {
	pid int32
	cpu float64
	mem float64
}

func (c *ProcessCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	procs, err := gpsproc.ProcessesWithContext(ctx)
	if err != nil {
		return err
	}

	samples := make([]procSample, 0, len(procs))
	var zombie uint64
	for _, p := range procs {
		// Status: filtra zombis para el contador y para muestrear CPU/mem.
		if status, err := p.StatusWithContext(ctx); err == nil {
			for _, s := range status {
				if s == gpsproc.Zombie {
					zombie++
				}
			}
		}
		cpu, _ := p.CPUPercentWithContext(ctx)
		mem, _ := p.MemoryPercentWithContext(ctx)
		samples = append(samples, procSample{pid: p.Pid, cpu: cpu, mem: float64(mem)})
	}

	// Top CPU
	sort.Slice(samples, func(i, j int) bool { return samples[i].cpu > samples[j].cpu })
	topCPU := toProcessInfo(samples, true)

	// Top Mem
	sort.Slice(samples, func(i, j int) bool { return samples[i].mem > samples[j].mem })
	topMem := toProcessInfo(samples, false)

	m.Processes = &payload.ProcessMetrics{
		Total:  uint64(len(procs)),
		Zombie: zombie,
		TopCPU: topCPU,
		TopMem: topMem,
	}
	return nil
}

func toProcessInfo(samples []procSample, byCPU bool) []payload.ProcessInfo {
	n := topN
	if len(samples) < n {
		n = len(samples)
	}
	out := make([]payload.ProcessInfo, 0, n)
	for i := 0; i < n; i++ {
		s := samples[i]
		name, _ := gpsproc.NewProcessWithContext(context.Background(), s.pid)
		nm := ""
		if name != nil {
			nm, _ = name.NameWithContext(context.Background())
		}
		pi := payload.ProcessInfo{PID: s.pid, Name: nm}
		if byCPU {
			pi.CPUPct = round1(s.cpu)
		} else {
			pi.MemPct = round1(s.mem)
		}
		out = append(out, pi)
	}
	return out
}