package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	gpsmem "github.com/shirou/gopsutil/v4/mem"
)

type MemoryCollector struct{}

func NewMemory() Collector { return &MemoryCollector{} }

func (c *MemoryCollector) Name() string { return "memory" }

func (c *MemoryCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	vm, err := gpsmem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return err
	}
	out := &payload.MemoryMetrics{
		TotalBytes: vm.Total,
		UsedBytes:  vm.Used,
		UsagePct:   roundPct(vm.UsedPercent),
	}
	if sm, err := gpsmem.SwapMemoryWithContext(ctx); err == nil {
		out.SwapUsedPct = roundPct(sm.UsedPercent)
	}
	m.Memory = out
	return nil
}
