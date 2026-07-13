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

func TestHost_UptimeSecIsZero(t *testing.T) {
	h := Host()
	if h.UptimeSec != 0 {
		t.Errorf("UptimeSec = %d, want 0 (not yet implemented)", h.UptimeSec)
	}
}

func TestHost_KernelIsEmpty(t *testing.T) {
	h := Host()
	if h.Kernel != "" {
		t.Errorf("Kernel = %q, want empty (not yet implemented)", h.Kernel)
	}
}
