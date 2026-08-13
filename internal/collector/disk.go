package collector

import (
	"context"
	"strings"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	gpsdisk "github.com/shirou/gopsutil/v4/disk"
)

type DiskCollector struct{}

func NewDisk() Collector { return &DiskCollector{} }

func (c *DiskCollector) Name() string { return "disk" }

// forbiddenFstype son los sistemas de archivos virtuales / pseudo que no
// aportan información útil de disco y conviene filtrar para no inflar el
// payload (loops, cgroups, /proc, /sys...). Lista típica de node_exporter.
var forbiddenFstype = map[string]struct{}{
	"tmpfs":           {},
	"devtmpfs":        {},
	"devfs":           {},
	"iso9660":         {},
	"overlay":         {},
	"aufs":            {},
	"squashfs":        {},
	"proc":            {},
	"sysfs":           {},
	"cgroup":          {},
	"cgroup2":         {},
	"nsfs":            {},
	"fuse.gvfsd-fuse": {},
	"fusectl":         {},
	"debugfs":         {},
	"tracefs":         {},
	"mqueue":          {},
	"hugetlbfs":       {},
	"rpc_pipefs":      {},
	"binfmt_misc":     {},
}

func isPseudoMount(m gpsdisk.PartitionStat) bool {
	if _, ok := forbiddenFstype[m.Fstype]; ok {
		return true
	}
	for _, p := range []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/snap"} {
		if m.Mountpoint == p || strings.HasPrefix(m.Mountpoint, p+"/") {
			return true
		}
	}
	return false
}

func (c *DiskCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	parts, err := gpsdisk.PartitionsWithContext(ctx, true)
	if err != nil {
		return err
	}
	var out []payload.DiskMetrics
	for _, p := range parts {
		if isPseudoMount(p) {
			continue
		}
		u, err := gpsdisk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue // degradar con elegancia (§5): omite este mount, no crashee
		}
		out = append(out, payload.DiskMetrics{
			Mount:     p.Mountpoint,
			UsagePct:  roundPct(u.UsedPercent),
			FreeBytes: u.Free,
		})
	}
	if len(out) > 0 {
		m.Disk = out
	}
	return nil
}
