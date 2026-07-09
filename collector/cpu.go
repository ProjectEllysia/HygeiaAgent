package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type CPUCollector struct{}

func NewCPU() Collector { return &CPUCollector{} }

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// TODO: implementar con github.com/shirou/gopsutil/v4/cpu:
	//   cpu.Percent(time.Second, true)  -> perCorePct (y global = media)
	//   github.com/shirou/gopsutil/v4/load  -> load.Avg() -> Load1/5/15
	return nil
}
