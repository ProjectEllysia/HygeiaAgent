package collector

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestProcessCollector_Name(t *testing.T) {
	c := NewProcesses()
	if got := c.Name(); got != "processes" {
		t.Errorf("Name() = %q, want %q", got, "processes")
	}
}

// Esta prueba antes esperaba m.Processes == nil ("not yet implemented");
// quedó obsoleta al implementar el colector de verdad vía gopsutil y el
// merge con la rama de colectores la arrastró sin actualizar.
//
// topCpu/topMem se dejan sin comprobar aquí a propósito: el primer ciclo de
// ProcessCollector no tiene línea base de CPU todavía (§12.1 del plan) y
// omite topCpu deliberadamente, así que no hay un valor único "correcto"
// que afirmar sin acoplar el test a ese detalle interno.
func TestProcessCollector_Collect(t *testing.T) {
	c := NewProcesses()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Processes == nil {
		t.Fatal("m.Processes = nil, se esperaba que el colector lo rellenara")
	}
	if m.Processes.Total == 0 {
		t.Error("Total = 0, se esperaba al menos el propio proceso de test")
	}
	// TopCPU nunca debe ser nil (aunque esté vacío): un nil slice serializa
	// a JSON `null`, que el schema del backend rechaza con 422 al no
	// permitir null en un campo de lista (bug real detectado en producción).
	if m.Processes.TopCPU == nil {
		t.Error("TopCPU = nil, se esperaba slice vacío (nil serializa a JSON null)")
	}
}

// El escenario que motivó la normalización por núcleos: un proceso que
// satura varios núcleos acumula más segundos de CPU que segundos de reloj
// transcurridos. Sin dividir entre el número de núcleos, un minero de
// criptomonedas en un equipo de 8 núcleos reportaba cpuPct=800, el backend
// rechazaba el heartbeat ENTERO con un 422, el shipper lo clasificaba como
// permanente y lo descartaba: el activo se quedaba mudo justo cuando tenía
// el problema que se quería detectar.
func TestCPURate_NormalizaPorNúcleos(t *testing.T) {
	cases := map[string]struct {
		deltaSec   float64 // segundos de CPU consumidos (suma de todos los núcleos)
		elapsedSec float64 // segundos de reloj entre ciclos
		cores      int
		want       float64
	}{
		"un núcleo al máximo de ocho":     {deltaSec: 15, elapsedSec: 15, cores: 8, want: 12.5},
		"ocho núcleos al máximo de ocho":  {deltaSec: 120, elapsedSec: 15, cores: 8, want: 100},
		"dos núcleos al máximo de cuatro": {deltaSec: 30, elapsedSec: 15, cores: 4, want: 50},
		"proceso inactivo":                {deltaSec: 0, elapsedSec: 15, cores: 4, want: 0},
		"equipo de un solo núcleo":        {deltaSec: 15, elapsedSec: 15, cores: 1, want: 100},
	}
	for name, c := range cases {
		if got := cpuRate(c.deltaSec, c.elapsedSec, c.cores); got != c.want {
			t.Errorf("%s: cpuRate(%v, %v, %d) = %v, se esperaba %v",
				name, c.deltaSec, c.elapsedSec, c.cores, got, c.want)
		}
	}
}

// Sin estos guardias, un elapsed de cero (dos ciclos en el mismo instante
// por un salto de reloj) produciría ±Inf, y un cores de cero una división
// por cero. roundPct los recortaría igualmente, pero es más honesto no
// generar el valor imposible que fabricarlo y taparlo después.
func TestCPURate_GuardsAgainstDegenerateInput(t *testing.T) {
	if got := cpuRate(10, 0, 8); got != 0 {
		t.Errorf("cpuRate con elapsed=0 = %v, se esperaba 0", got)
	}
	if got := cpuRate(10, 15, 0); got != 0 {
		t.Errorf("cpuRate con cores=0 = %v, se esperaba 0", got)
	}
}

// La prueba de extremo a extremo de A-01: la cadena completa desde el delta
// de CPU crudo hasta el valor que viaja en el JSON nunca puede salirse del
// rango que el contrato de ingesta admite, sea cual sea la entrada.
func TestCPURateSiemprePasaElContratoDeIngesta(t *testing.T) {
	// Casos deliberadamente absurdos: reloj que salta hacia atrás a mitad de
	// ciclo, contador de CPU que da la vuelta, máquina con un núcleo.
	cases := []struct {
		delta, elapsed float64
		cores          int
	}{
		{delta: 1e9, elapsed: 0.001, cores: 1},
		{delta: 120, elapsed: 15, cores: 8},
		{delta: 15, elapsed: 15, cores: 1},
		{delta: 0, elapsed: 15, cores: 4},
	}
	for _, c := range cases {
		got := roundPct(cpuRate(c.delta, c.elapsed, c.cores))
		if got < 0 || got > 100 {
			t.Errorf("cpuRate(%v, %v, %d) -> roundPct = %v, fuera de [0,100]",
				c.delta, c.elapsed, c.cores, got)
		}
	}
}

// insertTop sustituye a dos sort.Slice sobre el vector completo de procesos,
// y es aritmética de índices: el riesgo no es que sea lento, es que se
// equivoque en un desplazamiento. Esta prueba lo compara contra la
// implementación obvia (ordenar todo y cortar) sobre entradas aleatorias, que
// es la única forma de estar seguro sin razonar a mano cada caso.
func TestInsertTopMatchesAFullSort(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))

	for _, n := range []int{0, 1, 4, 5, 6, 50, 500} {
		for iter := 0; iter < 20; iter++ {
			all := make([]procSample, n)
			for i := range all {
				all[i] = procSample{pid: int32(i), cpu: math.Trunc(rng.Float64() * 1000)}
			}

			var top []procSample
			for _, s := range all {
				top = insertTop(top, s, byCPU)
			}

			// La referencia: ordenar el vector entero y quedarse con los topN
			// primeros, que es justo lo que hacía el código anterior.
			want := slices.Clone(all)
			sort.SliceStable(want, func(i, j int) bool { return want[i].cpu > want[j].cpu })
			if len(want) > topN {
				want = want[:topN]
			}

			if len(top) != len(want) {
				t.Fatalf("n=%d iter=%d: len(top) = %d, se esperaba %d", n, iter, len(top), len(want))
			}
			for i := range want {
				if top[i].cpu != want[i].cpu {
					t.Fatalf("n=%d iter=%d: top[%d].cpu = %v, se esperaba %v (top=%v)",
						n, iter, i, top[i].cpu, want[i].cpu, cpus(top))
				}
			}
		}
	}
}

func cpus(ss []procSample) []float64 {
	out := make([]float64, len(ss))
	for i, s := range ss {
		out[i] = s.cpu
	}
	return out
}

// El top se mantiene ordenado de mayor a menor en todo momento: el backend
// espera topCpu/topMem con el mayor primero, y la interfaz los pinta en ese
// orden sin reordenar.
func TestInsertTopKeepsDescendingOrder(t *testing.T) {
	var top []procSample
	for _, v := range []float64{3, 90, 1, 45, 45, 7, 100, 2} {
		top = insertTop(top, procSample{cpu: v}, byCPU)
	}
	if len(top) != topN {
		t.Fatalf("len(top) = %d, se esperaba %d", len(top), topN)
	}
	for i := 1; i < len(top); i++ {
		if top[i-1].cpu < top[i].cpu {
			t.Errorf("orden roto en %d: %v", i, cpus(top))
		}
	}
	if top[0].cpu != 100 {
		t.Errorf("top[0].cpu = %v, se esperaba 100", top[0].cpu)
	}
}

// Nunca crece por encima de topN, por muchos procesos que tenga el equipo:
// el backend rechaza el heartbeat si topCpu supera maxProcesses.
func TestInsertTopNeverExceedsTopN(t *testing.T) {
	var top []procSample
	for i := 0; i < 1000; i++ {
		top = insertTop(top, procSample{cpu: float64(i)}, byCPU)
		if len(top) > topN {
			t.Fatalf("len(top) = %d tras %d inserciones, el tope es %d", len(top), i+1, topN)
		}
	}
}

// Referencia de coste del colector completo, para poder comparar antes y
// después de quitarle llamadas al sistema por proceso (A-05).
func BenchmarkProcessCollectorCollect(b *testing.B) {
	c := NewProcesses()
	m := &payload.Metrics{}
	// Un ciclo previo, para que exista línea base de CPU y el benchmark mida
	// el camino normal y no el primer arranque, que omite topCpu.
	_ = c.Collect(context.Background(), m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Collect(context.Background(), m); err != nil {
			b.Fatal(err)
		}
	}
}
