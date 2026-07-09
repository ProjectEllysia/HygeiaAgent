package collector

import (
	"os"
	"runtime"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

// Host rellena la parte identificativa del payload (README §9, "host").
// hostname y os salen de la stdlib; kernel y uptime requieren gopsutil.
func Host() payload.HostInfo {
	h := payload.HostInfo{}
	if name, err := os.Hostname(); err == nil {
		h.Hostname = name
	}
	h.OS = runtime.GOOS
	// TODO: completar con github.com/shirou/gopsutil/v4/host:
	//   info, _ := host.Info()  -> info.KernelVersion, info.Uptime
	return h
}
