package collector

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// defaultDRMBasePath es la raíz de los dispositivos gráficos en Linux.
// Variable para que los tests la sustituyan por un árbol temporal.
var defaultDRMBasePath = "/sys/class/drm"

// amdGPUProvider lee la potencia media que el driver amdgpu publica bajo el
// hwmon asociado al dispositivo (P11): power1_average, en microvatios, en
// /sys/class/drm/cardN/device/hwmon/hwmonM/. A diferencia de P09 (hwmon
// general), aquí no hace falta una lista de drivers reconocidos: colgar del
// dispositivo drm concreto ya atribuye el sensor a la GPU sin ambigüedad.
type amdGPUProvider struct {
	basePath string
	log      *slog.Logger
}

func newAMDGPUProvider(log *slog.Logger) *amdGPUProvider {
	return &amdGPUProvider{basePath: defaultDRMBasePath, log: log}
}

func (p *amdGPUProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	matches, err := filepath.Glob(filepath.Join(p.basePath, "card*", "device", "hwmon", "hwmon*", "power1_average"))
	if err != nil || len(matches) == 0 {
		return nil, nil
	}

	var total float64
	var found bool
	for _, m := range matches {
		microWatts, ok := parseFloatTrimmed(readTrimmedFile(m))
		if !ok {
			continue
		}
		total += microWatts / 1e6
		found = true
	}
	if !found {
		return nil, nil
	}
	return &payload.PowerMetrics{Watts: total, Estimated: false, Source: "amd_gpu"}, nil
}
