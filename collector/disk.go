package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type DiskCollector struct{}

func NewDisk() Collector { return &DiskCollector{} }

func (c *DiskCollector) Name() string { return "disk" }

func (c *DiskCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// TODO: implementar con github.com/shirou/gopsutil/v4/disk:
	//   disk.Partitions(true) -> []PartitionStat (filtrar fiscales/loop)
	//   disk.Usage(mount)     -> .UsedPercent, .Free, .Total
	return nil
}
