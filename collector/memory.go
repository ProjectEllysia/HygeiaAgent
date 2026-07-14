package collector

import (
	"context"

	gpsmem "github.com/shirou/gopsutil/v4/mem"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
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
		UsagePct:   round1(vm.UsedPercent),
	}
	if sm, err := gpsmem.SwapMemoryWithContext(ctx); err == nil {
		out.SwapUsedPct = round1(sm.UsedPercent)
	}
	m.Memory = out
	return nil
}