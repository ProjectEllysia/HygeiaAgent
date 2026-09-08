//go:build linux || windows

package collector

import (
	"context"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// nvidiaSMITimeout acota cada invocación de nvidia-smi: el collector corre
// hasta cuatro veces por minuto y un driver en mal estado no debe poder
// colgar el ciclo de recolección (README, P04).
const nvidiaSMITimeout = 3 * time.Second

// nvidiaGPUProvider lee la potencia de GPU NVIDIA ejecutando nvidia-smi
// (P10), sin enlazar contra NVML: NVML exige cgo, y con cgo el agente deja de
// poder compilarse en cruzado con un simple `GOOS=... go build`.
//
// nvidia-smi se busca en el PATH una sola vez por proceso — la inmensa
// mayoría de máquinas no tiene GPU NVIDIA, y repetir esa búsqueda cuatro
// veces por minuto sería coste sin beneficio.
type nvidiaGPUProvider struct {
	log *slog.Logger

	lookPath func(string) (string, error)
	run      func(ctx context.Context, bin string) ([]byte, error)

	lookupOnce sync.Once
	binPath    string
}

func newNVIDIAGPUProvider(log *slog.Logger) *nvidiaGPUProvider {
	return &nvidiaGPUProvider{log: log, lookPath: exec.LookPath, run: runNvidiaSMI}
}

func runNvidiaSMI(ctx context.Context, bin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, "--query-gpu=power.draw", "--format=csv,noheader,nounits")
	return cmd.Output()
}

func (p *nvidiaGPUProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	p.lookupOnce.Do(func() {
		if path, err := p.lookPath("nvidia-smi"); err == nil {
			p.binPath = path
		}
	})
	if p.binPath == "" {
		return nil, nil
	}

	cctx, cancel := context.WithTimeout(ctx, nvidiaSMITimeout)
	defer cancel()

	out, err := p.run(cctx, p.binPath)
	if err != nil {
		// GPU presente pero el driver no respondió (o se agotó el timeout):
		// se omite este ciclo, no se rompe el heartbeat por ello.
		return nil, nil
	}

	total, ok := sumNvidiaSMIOutput(string(out))
	if !ok {
		return nil, nil
	}
	return &payload.PowerMetrics{Watts: total, Estimated: false, Source: "nvidia"}, nil
}

// sumNvidiaSMIOutput suma la potencia de cada GPU listada, una por línea
// (--format=csv,noheader,nounits deja solo el número).
func sumNvidiaSMIOutput(out string) (float64, bool) {
	var total float64
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		total += v
		found = true
	}
	return total, found
}
