package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type MemoryCollector struct{}

func NewMemory() Collector { return &MemoryCollector{} }

func (c *MemoryCollector) Name() string { return "memory" }

func (c *MemoryCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// TODO: implementar con github.com/shirou/gopsutil/v4/mem:
	//   mem.VirtualMemory() -> .Total, .Used, .UsedPercent
	//   mem.SwapMemory()    -> .UsedPercent
	return nil
}
