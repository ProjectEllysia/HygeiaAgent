package collector

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	gpsmem "github.com/shirou/gopsutil/v4/mem"
	gpsproc "github.com/shirou/gopsutil/v4/process"
)

// topN es el número de procesos que se reportan en topCpu/topMem (§3).
const topN = 5

// numCPU es el número de núcleos lógicos del equipo. Se captura una vez: no
// cambia durante la vida del proceso, y es el divisor que cpuRate necesita.
var numCPU = runtime.NumCPU()

// cpuRate convierte el consumo de CPU de un proceso entre dos ciclos en
// porcentaje de la capacidad TOTAL del equipo.
//
// deltaSec es tiempo de CPU, y ese tiempo suma todos los núcleos: un proceso
// que satura cuatro núcleos durante diez segundos de reloj acumula cuarenta
// segundos de CPU. Dividir solo entre el tiempo transcurrido daría 400, que
// es la semántica de `top` — legítima, pero incompatible con el contrato de
// ingesta, que acota cpuPct a [0,100] y rechaza el heartbeat entero si se
// sale (ver roundPct en cpu.go).
//
// Al dividir además entre el número de núcleos, 100 pasa a significar "este
// proceso tiene la máquina entera para él". Es la misma escala que
// metrics.cpu.usagePct (media de todos los núcleos), así que ambas cifras se
// pueden comparar entre sí en el panel: un usagePct de 90 con un proceso al
// 85 dice que ese proceso ES la carga; el mismo usagePct con todo el top por
// debajo de 5 dice que la carga está repartida.
func cpuRate(deltaSec, elapsedSec float64, cores int) float64 {
	if elapsedSec <= 0 || cores <= 0 {
		return 0
	}
	return 100 * deltaSec / (elapsedSec * float64(cores))
}

// ProcessCollector guarda el tiempo de CPU acumulado de cada PID entre
// ciclos (mismo patrón que NetworkCollector con las interfaces) para poder
// calcular una TASA reciente de uso de CPU, en vez de la media histórica
// que da gopsutil de fábrica (ver comentario de Collect, plan §12.1).
type ProcessCollector struct {
	mu       sync.Mutex
	prevCPU  map[int32]float64 // pid -> segundos de CPU totales, ciclo anterior
	prevTime time.Time
}

func NewProcesses() Collector {
	return &ProcessCollector{prevCPU: make(map[int32]float64)}
}

func (c *ProcessCollector) Name() string { return "processes" }

type procSample struct {
	pid  int32
	proc *gpsproc.Process
	cpu  float64
	mem  float64
}

// Collect recorre todos los procesos y arma el top-N por CPU y por memoria
// (§3).
//
// El % de CPU se calcula como delta de tiempo de CPU entre este ciclo y el
// anterior dividido por el tiempo transcurrido — el mismo patrón que
// network.go ya usa para las interfaces — en vez de
// Process.CPUPercentWithContext() de gopsutil, que divide el tiempo de CPU
// TOTAL entre el tiempo transcurrido DESDE QUE EL PROCESO ARRANCÓ: es una
// media de toda la vida del proceso, no el uso reciente. Un proceso de días
// que empieza a picar al 100 % ahora mismo saldría con un cpuPct casi cero
// bajo ese cálculo — justo el escenario que el §3 pone como ejemplo de por
// qué existe el top-N (plan §12.1).
//
// El % de memoria se calcula aquí en vez de con Process.MemoryPercent() de
// gopsutil, que consulta la memoria TOTAL del sistema en cada llamada (ver
// su implementación: mem.VirtualMemory() + p.MemoryInfo()). En un equipo con
// trescientos procesos eso son trescientas consultas del mismo dato
// invariante — en Linux, trescientas lecturas de /proc/meminfo por ciclo.
// Se lee una vez, fuera del bucle.
func (c *ProcessCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	procs, err := gpsproc.ProcessesWithContext(ctx)
	if err != nil {
		return err
	}

	// La memoria total no cambia durante el ciclo. Si falla, memPct se queda
	// a cero para todos y el resto del colector sigue: degradar con elegancia
	// (§5), no tumbar el heartbeat por un dato secundario.
	var totalMem float64
	if vm, err := gpsmem.VirtualMemoryWithContext(ctx); err == nil {
		totalMem = float64(vm.Total)
	}

	now := time.Now()
	c.mu.Lock()
	prevCPU := c.prevCPU
	prevTime := c.prevTime
	c.mu.Unlock()

	elapsed := now.Sub(prevTime).Seconds()
	// El primer ciclo (o uno tras un hueco extraño de reloj) no tiene
	// referencia: todas las tasas de CPU serían 0 y el "top" sería
	// arbitrario, así que se omite el top-N por CPU ese ciclo — igual que
	// network.go omite las interfaces nuevas.
	haveBaseline := elapsed > 0 && len(prevCPU) > 0

	newCPU := make(map[int32]float64, len(procs))
	// Los dos tops se construyen sobre la marcha, quedándose solo con los
	// topN mayores de cada criterio: no hace falta materializar los cientos
	// de procesos del equipo ni ordenarlos dos veces para elegir diez.
	var topCPU, topMem []procSample
	var zombie uint64

	for _, p := range procs {
		if zombieStatusAvailable {
			if status, err := p.StatusWithContext(ctx); err == nil {
				for _, s := range status {
					if s == gpsproc.Zombie {
						zombie++
					}
				}
			}
		}

		var cpuPct float64
		if cput, err := p.TimesWithContext(ctx); err == nil {
			total := cpuTotal(*cput)
			newCPU[p.Pid] = total
			if haveBaseline {
				if prev, ok := prevCPU[p.Pid]; ok && total >= prev {
					cpuPct = cpuRate(total-prev, elapsed, numCPU)
				}
			}
		}

		var memPct float64
		if totalMem > 0 {
			if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
				memPct = 100 * float64(mi.RSS) / totalMem
			}
		}

		s := procSample{pid: p.Pid, proc: p, cpu: cpuPct, mem: memPct}
		if haveBaseline {
			topCPU = insertTop(topCPU, s, byCPU)
		}
		topMem = insertTop(topMem, s, byMem)
	}

	c.mu.Lock()
	c.prevCPU = newCPU
	c.prevTime = now
	c.mu.Unlock()

	m.Processes = &payload.ProcessMetrics{
		Total:  uint64(len(procs)),
		Zombie: zombie,
		TopCPU: toProcessInfo(ctx, topCPU, true),
		TopMem: toProcessInfo(ctx, topMem, false),
	}
	return nil
}

// zombieStatusAvailable: en Windows, gopsutil devuelve siempre
// ErrNotImplementedError desde StatusWithContext (process_windows.go), así
// que el recuento de zombis sale cero de todas formas y la llamada solo
// gasta trabajo por proceso. En sistemas tipo Unix sí es un dato real, y
// cuesta una lectura de /proc/<pid>/status por proceso, así que se paga solo
// donde sirve para algo. El concepto de proceso zombi tampoco existe en
// Windows, de modo que no se pierde nada.
var zombieStatusAvailable = runtime.GOOS != "windows"

func byCPU(s procSample) float64 { return s.cpu }
func byMem(s procSample) float64 { return s.mem }

// insertTop mantiene `top` ordenado de mayor a menor con como mucho topN
// elementos, insertando `s` si entra.
//
// Sustituye a dos sort.Slice sobre el vector completo de procesos. Con topN=5
// una inserción lineal en un vector de cinco es más simple y más barata que
// un montículo, y sobre todo evita tener que guardar los cientos de procesos
// del equipo solo para quedarse con diez.
func insertTop(top []procSample, s procSample, value func(procSample) float64) []procSample {
	v := value(s)
	if len(top) == topN && v <= value(top[topN-1]) {
		return top // no entra: ni siquiera supera al menor de los que ya están
	}

	i := sort.Search(len(top), func(j int) bool { return value(top[j]) < v })
	if len(top) < topN {
		top = append(top, s) // crece una posición; el valor real se coloca abajo
	}
	// Desplaza a la derecha desde el punto de inserción. Cuando el vector ya
	// estaba lleno, esto tira al que ocupaba la última posición.
	copy(top[i+1:], top[i:len(top)-1])
	top[i] = s
	return top
}

// toProcessInfo lee el nombre solo de los N procesos que ya se sabe que
// entran en el top, reusando el *process.Process obtenido en Collect en vez
// de recrearlo por PID (evita una revalidación de existencia redundante;
// plan §12.2, Tier 2).
//
// Nunca devuelve nil aunque `samples` esté vacío: un nil slice serializa a
// JSON `null` y el schema del backend rechaza null en un campo de lista.
func toProcessInfo(ctx context.Context, samples []procSample, isCPU bool) []payload.ProcessInfo {
	out := make([]payload.ProcessInfo, 0, len(samples))
	for _, s := range samples {
		name, _ := s.proc.NameWithContext(ctx)
		pi := payload.ProcessInfo{PID: s.pid, Name: name}
		if isCPU {
			pi.CPUPct = roundPct(s.cpu)
		} else {
			pi.MemPct = roundPct(s.mem)
		}
		out = append(out, pi)
	}
	return out
}
