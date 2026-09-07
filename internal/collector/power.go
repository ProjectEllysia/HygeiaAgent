package collector

import (
	"context"
	"log/slog"
	"sync"

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
//
// La mayoría de las máquinas donde se instale este agente no van a exponer
// ninguna fuente de potencia utilizable — eso es el caso NORMAL, no el
// excepcional (un contenedor sin powercap montado, una máquina virtual, un
// Windows cualquiera) —, así que Collect nunca deja que la ausencia de
// fuente, ni un fallo de la que hubiera, tumben el heartbeat entero: ese
// riesgo dejaría mudos a activos que hoy reportan CPU y memoria sin
// problema, a cambio de una métrica nueva. El aviso correspondiente se
// registra una única vez (warnOnce), no en cada ciclo: con el intervalo por
// defecto son cuatro heartbeats por minuto, y repetir la misma línea para
// siempre llenaría el log sin aportar nada pasado el primer aviso.
type PowerCollector struct {
	provider PowerProvider
	log      *slog.Logger

	warnOnce sync.Once
}

// NewPower construye el collector con el proveedor de esta plataforma. log
// es el logger del agente, para el aviso único de "sin fuente" descrito
// arriba.
//
// Esta fase (Fase 0 del proyecto de consumo energético) no lee ningún sensor
// todavía: newPowerProvider devuelve un proveedor que siempre dice "sin
// fuente". Los proveedores reales de Linux y Windows llegan en la Fase 1.
func NewPower(log *slog.Logger) Collector {
	return &PowerCollector{provider: newPowerProvider(), log: log}
}

func (c *PowerCollector) Name() string { return "power" }

func (c *PowerCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	pw, err := c.provider.Read(ctx)
	switch {
	case err != nil:
		c.warnOnce.Do(func() {
			c.log.Warn("consumo eléctrico no disponible: la fuente de potencia falló", "err", err)
		})
		return nil
	case pw == nil:
		c.warnOnce.Do(func() {
			c.log.Info("esta máquina no expone ninguna fuente de consumo eléctrico compatible")
		})
		return nil
	default:
		m.Power = pw
		return nil
	}
}

// noopPowerProvider es el proveedor por defecto mientras no exista ninguna
// implementación específica de sistema operativo: nunca encuentra fuente.
type noopPowerProvider struct{}

func (noopPowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	return nil, nil
}

func newPowerProvider() PowerProvider { return noopPowerProvider{} }
