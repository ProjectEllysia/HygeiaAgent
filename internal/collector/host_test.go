package collector

import (
	"os"
	"runtime"
	"testing"
)

func TestHost_ReturnsHostname(t *testing.T) {
	h := Host()
	expected, _ := os.Hostname()
	if h.Hostname != expected {
		t.Errorf("Hostname = %q, want %q", h.Hostname, expected)
	}
}

func TestHost_ReturnsOS(t *testing.T) {
	h := Host()
	if h.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", h.OS, runtime.GOOS)
	}
}

// Kernel y UptimeSec vienen de gopsutil (host.Info/Uptime), no de la
// stdlib: en cualquier SO real deberían venir rellenos. Estas dos
// aserciones antes esperaban lo contrario ("not yet implemented"), de
// cuando Host() todavía no llamaba a gopsutil — quedaron obsoletas al
// implementarlo y el merge con la rama de colectores las arrastró.
func TestHost_UptimeSecIsPopulated(t *testing.T) {
	h := Host()
	if h.UptimeSec == 0 {
		t.Error("UptimeSec = 0, se esperaba > 0 (gopsutil.host.Uptime)")
	}
}

func TestHost_KernelIsPopulated(t *testing.T) {
	h := Host()
	if h.Kernel == "" {
		t.Error("Kernel = \"\", se esperaba la versión de kernel/SO (gopsutil.host.Info)")
	}
}
