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
	// firstCycleDone separa "todavía no hay dato" de "aquí no hay fuente".
	// Una fuente basada en un contador de energía acumulada (RAPL es la
	// principal) no puede dar vatios en su primer ciclo: necesita dos
	// lecturas y el tiempo entre ellas, así que devuelve nil sin error, que
	// es exactamente lo que devuelve una máquina sin sensores. Avisar ahí
	// gastaría warnOnce en el único ciclo en que la afirmación siempre es
	// falsa, y ya no habría forma de retractarse.
	firstCycleDone bool
}

// NewPower construye el collector con el proveedor de esta plataforma. log
// es el logger del agente, para el aviso único de "sin fuente" descrito
// arriba, y se pasa también al proveedor: cada fuente real (RAPL, hwmon,
// GPU...) tiene su propia razón para no encontrar nada, y necesita el mismo
// logger para avisar UNA vez de la suya (Fase 1, P08).
func NewPower(log *slog.Logger) Collector {
	return &PowerCollector{provider: newPowerProvider(log), log: log}
}

func (c *PowerCollector) Name() string { return "power" }

func (c *PowerCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	firstCycle := !c.firstCycleDone
	c.firstCycleDone = true

	pw, err := c.provider.Read(ctx)
	switch {
	case err != nil:
		c.warnOnce.Do(func() {
			c.log.Warn("consumo eléctrico no disponible: la fuente de potencia falló", "err", err)
		})
		return nil
	case pw == nil:
		// En una máquina sin ninguna fuente el aviso solo se retrasa un
		// ciclo (15 s con la cadencia por defecto); en una con RAPL deja de
		// aparecer, que es lo que se quiere.
		if !firstCycle {
			c.warnOnce.Do(func() {
				c.log.Info("esta máquina no expone ninguna fuente de consumo eléctrico compatible")
			})
		}
		return nil
	default:
		m.Power = pw
		return nil
	}
}

// newPowerProvider construye el proveedor real de esta plataforma. Vive en
// power_linux.go, power_windows.go y power_other.go, uno por cada valor de
// runtime.GOOS que el nombre de fichero selecciona en tiempo de compilación
// (el mismo mecanismo que ya usa el inventario: inventory_linux.go,
// inventory_windows.go, inventory_darwin.go).
//
// PowerDiagnostic y DiagnosePower, usados por `doctor` (P08), siguen el mismo
// reparto por fichero.
type PowerDiagnostic struct {
	// Available indica si esta máquina tiene, ahora mismo, una fuente de
	// potencia utilizable — no si algún día podría tenerla.
	Available bool
	// Detail es una línea legible sobre qué se encontró (o no).
	Detail string
	// Hint solo se rellena cuando hay algo que hacer al respecto (falta un
	// privilegio, falta un binario...). Vacío en el caso normal de una
	// máquina sin sensores compatibles: ahí no hay ninguna acción que sugerir.
	Hint string
}

// DiagnosePower resume qué fuente de potencia ve el agente en esta máquina y,
// si no ve ninguna, por qué. `doctor` lo usa para que la ausencia de
// electricidad en el heartbeat sea diagnosticable sin leer el código
// (Fase 1, P08): decir "no hay power" no basta, hace falta decir POR QUÉ.
func DiagnosePower() PowerDiagnostic { return diagnosePower() }
