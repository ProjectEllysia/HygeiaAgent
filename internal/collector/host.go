package collector

import (
	"os"
	"runtime"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	gpshost "github.com/shirou/gopsutil/v4/host"
)

// Host rellena la parte identificativa del payload (README §9, "host").
// hostname y os salen de la stdlib; kernel y uptime se obtienen de gopsutil.
// Si gopsutil falla (p. ej. sin permisos en algún SO), se degradan a cero y
// no se impide el envío del resto del heartbeat.
func Host() payload.HostInfo {
	h := payload.HostInfo{}
	if name, err := os.Hostname(); err == nil {
		h.Hostname = name
	}
	h.OS = runtime.GOOS
	if info, err := gpshost.Info(); err == nil {
		h.Kernel = info.KernelVersion
		h.UptimeSec = info.Uptime
	} else if kv, err := gpshost.KernelVersion(); err == nil {
		h.Kernel = kv
	}
	if h.UptimeSec == 0 {
		if up, err := gpshost.Uptime(); err == nil {
			h.UptimeSec = up
		}
	}
	return h
}
