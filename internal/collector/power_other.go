//go:build !linux && !windows

package collector

import (
	"context"
	"log/slog"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// noopPowerProvider es el proveedor del resto de plataformas (macOS): no hay
// ninguna implementación específica todavía —ni RAPL ni el modelo de Windows
// aplican aquí— y nunca encuentra fuente.
type noopPowerProvider struct{}

func (noopPowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	return nil, nil
}

func newPowerProvider(log *slog.Logger) PowerProvider { return noopPowerProvider{} }

func diagnosePower() PowerDiagnostic {
	return PowerDiagnostic{Available: false, Detail: "esta plataforma no tiene proveedor de potencia implementado"}
}
