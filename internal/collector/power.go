package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// PowerProvider aísla de dónde sale el vatio: cada sistema operativo, e
// incluso cada sensor dentro del mismo sistema, tiene su propia forma de
// leerlo (RAPL, hwmon, NVML, un modelo de estimación...). El collector no
// sabe nada de eso; solo sabe hablar con esta interfaz.
type PowerProvider interface {
	// Read devuelve la potencia actual, o nil sin error cuando esta máquina
	// no expone ninguna fuente compatible. El error se reserva para el fallo
	// de una fuente que sí existía (por ejemplo, un fichero que dejó de
	// poder leerse).
	Read(ctx context.Context) (*payload.PowerMetrics, error)
}

// PowerCollector adapta un PowerProvider al Collector genérico del agente.
type PowerCollector struct {
	provider PowerProvider
}

// NewPower construye el collector con el proveedor de esta plataforma.
//
// Esta fase (Fase 0 del proyecto de consumo energético) no lee ningún sensor
// todavía: newPowerProvider devuelve un proveedor que siempre dice "sin
// fuente". Los proveedores reales de Linux y Windows llegan en la Fase 1.
func NewPower() Collector {
	return &PowerCollector{provider: newPowerProvider()}
}

func (c *PowerCollector) Name() string { return "power" }

func (c *PowerCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	pw, err := c.provider.Read(ctx)
	if err != nil {
		return err
	}
	if pw == nil {
		return nil
	}
	m.Power = pw
	return nil
}

// noopPowerProvider es el proveedor por defecto mientras no exista ninguna
// implementación específica de sistema operativo: nunca encuentra fuente.
type noopPowerProvider struct{}

func (noopPowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	return nil, nil
}

func newPowerProvider() PowerProvider { return noopPowerProvider{} }
