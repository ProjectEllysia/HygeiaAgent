package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type ProcessCollector struct{}

func NewProcesses() Collector { return &ProcessCollector{} }

func (c *ProcessCollector) Name() string { return "processes" }

func (c *ProcessCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// TODO: implementar con github.com/shirou/gopsutil/v4/process:
	//   process.Processes() -> []*Process
	//   por cada p: p.Name(), p.CPUPercent(), p.MemoryPercent()
	//   contar total/zombies y quedarse con el top-N por CPU y por memoria.
	return nil
}
