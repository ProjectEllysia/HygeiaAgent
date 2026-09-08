package collector

import (
	"context"
	"log/slog"
	"regexp"
	"sync"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	gpscpu "github.com/shirou/gopsutil/v4/cpu"
)

func newPowerProvider(log *slog.Logger) PowerProvider { return newWindowsPowerProvider(log) }

// idleBaseWatts es el consumo aproximado de un equipo típico en reposo —
// placa, RAM, ventiladores, discos—, fuera del paquete de CPU. Sin él, un
// equipo con la CPU al 0% estimaría 0 W, tan falso como el número que este
// modelo existe para evitar.
const idleBaseWatts = 15.0

// cpuFamilyPatterns asocia una familia de CPU con su TDP de fábrica en
// vatios, de la más específica a la más genérica (P13). Es deliberadamente
// pequeña: cubre las familias más comunes en equipos de escritorio, portátil
// y servidor, y una CPU que no encaja en ninguna degrada a "sin fuente" en
// vez de estimar con un TDP inventado — la tabla completa, y por qué Windows
// necesita este modelo en lugar de medir, están documentadas en el README.
var cpuFamilyPatterns = []struct {
	re  *regexp.Regexp
	tdp float64
}{
	{regexp.MustCompile(`(?i)Threadripper`), 280},
	{regexp.MustCompile(`(?i)Ryzen\s*9`), 105},
	{regexp.MustCompile(`(?i)Ryzen\s*7`), 65},
	{regexp.MustCompile(`(?i)Ryzen\s*5`), 65},
	{regexp.MustCompile(`(?i)Ryzen\s*3`), 65},
	{regexp.MustCompile(`(?i)EPYC`), 225},
	{regexp.MustCompile(`(?i)Xeon`), 150},
	{regexp.MustCompile(`(?i)\bi9\b`), 125},
	{regexp.MustCompile(`(?i)\bi7\b`), 65},
	{regexp.MustCompile(`(?i)\bi5\b`), 65},
	{regexp.MustCompile(`(?i)\bi3\b`), 60},
	{regexp.MustCompile(`(?i)Pentium`), 35},
	{regexp.MustCompile(`(?i)Celeron`), 15},
	{regexp.MustCompile(`(?i)Atom\b`), 10},
}

// tdpForModel busca la primera familia cuyo patrón aparece en el nombre de
// modelo que gopsutil reporta (cpu.InfoStat.ModelName). ok=false cuando
// ninguna familia conocida encaja.
func tdpForModel(model string) (tdp float64, ok bool) {
	for _, p := range cpuFamilyPatterns {
		if p.re.MatchString(model) {
			return p.tdp, true
		}
	}
	return 0, false
}

// estimateWattsFromUtilization aplica el modelo: consumo base en reposo más
// una fracción del TDP proporcional a la utilización.
func estimateWattsFromUtilization(tdp, utilizationPct float64) float64 {
	switch {
	case utilizationPct < 0:
		utilizationPct = 0
	case utilizationPct > 100:
		utilizationPct = 100
	}
	return idleBaseWatts + tdp*(utilizationPct/100)
}

// windowsPowerProvider estima la potencia por modelo de utilización (P13):
// Windows no expone ningún contador de usuario para energía de CPU, y la
// única vía técnica —un driver de kernel leyendo MSR— es inaceptable en un
// agente de seguridad (ver P14 y el README, sección del collector power).
//
// La GPU NVIDIA, cuando existe, se mide de verdad vía nvidia-smi (el mismo
// proveedor que usa Linux) y se suma a la estimación de CPU.
type windowsPowerProvider struct {
	log *slog.Logger
	gpu *nvidiaGPUProvider

	cpuModel func() (string, error)
	cpuUsage func(ctx context.Context) (float64, error)

	warnUnknownCPUOnce sync.Once
}

func newWindowsPowerProvider(log *slog.Logger) *windowsPowerProvider {
	return &windowsPowerProvider{
		log:      log,
		gpu:      newNVIDIAGPUProvider(log),
		cpuModel: defaultCPUModel,
		cpuUsage: defaultCPUUsage,
	}
}

// defaultCPUModel es un var (no un func) para que los tests de diagnosePower
// puedan sustituirlo sin depender de la CPU real de la máquina que ejecuta la
// suite.
var defaultCPUModel = func() (string, error) {
	infos, err := gpscpu.Info()
	if err != nil || len(infos) == 0 {
		return "", err
	}
	return infos[0].ModelName, nil
}

// defaultCPUUsage usa intervalo 0: gopsutil devuelve entonces la utilización
// desde la última llamada, sin bloquear. La primera lectura de la vida del
// proceso puede salir imprecisa (no hay "última llamada" previa); es
// aceptable para una estimación que ya se declara como tal.
func defaultCPUUsage(ctx context.Context) (float64, error) {
	pct, err := gpscpu.PercentWithContext(ctx, 0, false)
	if err != nil || len(pct) == 0 {
		return 0, err
	}
	return pct[0], nil
}

func (p *windowsPowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	model, err := p.cpuModel()
	if err != nil || model == "" {
		return nil, nil
	}
	tdp, ok := tdpForModel(model)
	if !ok {
		p.warnUnknownCPUOnce.Do(func() {
			p.log.Info("consumo eléctrico no disponible: CPU no reconocida en la tabla de TDP", "model", model)
		})
		return nil, nil
	}

	usage, err := p.cpuUsage(ctx)
	if err != nil {
		return nil, nil
	}

	watts := estimateWattsFromUtilization(tdp, usage)
	source := "model"
	if gpu, _ := p.gpu.Read(ctx); gpu != nil {
		watts += gpu.Watts
		source = "model+nvidia"
	}
	return &payload.PowerMetrics{Watts: watts, Estimated: true, Source: source}, nil
}

// diagnosePower implementa DiagnosePower (power.go) para Windows: siempre
// sale como "disponible" con estimated=true en cuanto la CPU está en la
// tabla, porque eso es justo lo único que Windows puede ofrecer sin driver de
// kernel (P13/P14) — no hay un caso "sin privilegio" que reportar aquí, a
// diferencia de RAPL en Linux.
func diagnosePower() PowerDiagnostic {
	model, err := defaultCPUModel()
	if err != nil || model == "" {
		return PowerDiagnostic{Available: false, Detail: "no se pudo identificar la CPU"}
	}
	if _, ok := tdpForModel(model); !ok {
		return PowerDiagnostic{
			Available: false,
			Detail:    "CPU no reconocida en la tabla de TDP (" + model + ")",
			Hint:      "Windows no mide potencia sin un driver de kernel (ver README, collector power). Añade esta CPU a la tabla para una estimación.",
		}
	}
	return PowerDiagnostic{
		Available: true,
		Detail:    "estimación por modelo de utilización (source=model)",
		Hint:      "No es una medición: Windows no expone potencia de CPU sin un driver de kernel. Ver README, sección del collector power.",
	}
}
