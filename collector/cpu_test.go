package collector

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	gpscpu "github.com/shirou/gopsutil/v4/cpu"
)

func TestRoundPct(t *testing.T) {
	if got := roundPct(87.549); got != 87.5 {
		t.Errorf("roundPct(87.549) = %v, se esperaba 87.5", got)
	}
	if got := roundPct(0); got != 0 {
		t.Errorf("roundPct(0) = %v, se esperaba 0", got)
	}
}

// El cast a int64 de un valor no finito es indefinido en Go — roundPct debe
// devolver un valor del rango en vez de propagar basura al payload
// (plan §12.2, Tier 2).
func TestRoundPctGuardsAgainstNonFiniteInput(t *testing.T) {
	if got := roundPct(math.NaN()); got != 0 {
		t.Errorf("roundPct(NaN) = %v, se esperaba 0", got)
	}
	if got := roundPct(math.Inf(1)); got != 100 {
		t.Errorf("roundPct(+Inf) = %v, se esperaba 100", got)
	}
	if got := roundPct(math.Inf(-1)); got != 0 {
		t.Errorf("roundPct(-Inf) = %v, se esperaba 0", got)
	}
}

// El backend valida CADA porcentaje del payload con Range(min=0, max=100) y
// rechaza el heartbeat entero con un 422 si uno solo se sale — y el shipper
// clasifica ese 422 como permanente, así que el ciclo se descarta sin pasar
// por el buffer. roundPct es el único punto por el que pasan todos los
// porcentajes, y es donde se corta.
func TestRoundPctClampsToIngestContractRange(t *testing.T) {
	cases := map[string]struct {
		in   float64
		want float64
	}{
		"proceso saturando 2 núcleos": {in: 200, want: 100},
		"proceso saturando 8 núcleos": {in: 800, want: 100},
		"justo en el límite":          {in: 100, want: 100},
		"justo por encima":            {in: 100.04, want: 100},
		"negativo por salto de reloj": {in: -12.5, want: 0},
		"dentro de rango":             {in: 99.94, want: 99.9},
	}
	for name, c := range cases {
		if got := roundPct(c.in); got != c.want {
			t.Errorf("%s: roundPct(%v) = %v, se esperaba %v", name, c.in, got, c.want)
		}
	}
}

// deltaPct es la sustitución del muestreo bloqueante: se prueba con
// contadores sintéticos, sin depender de la carga real de la máquina.
func TestDeltaPct(t *testing.T) {
	cases := map[string]struct {
		prev, cur gpscpu.TimesStat
		want      float64
	}{
		"núcleo ocioso todo el intervalo": {
			prev: gpscpu.TimesStat{User: 100, Idle: 900},
			cur:  gpscpu.TimesStat{User: 100, Idle: 1000},
			want: 0,
		},
		"núcleo saturado todo el intervalo": {
			prev: gpscpu.TimesStat{User: 100, Idle: 900},
			cur:  gpscpu.TimesStat{User: 200, Idle: 900},
			want: 100,
		},
		"núcleo al 25 %": {
			prev: gpscpu.TimesStat{User: 100, Idle: 900},
			cur:  gpscpu.TimesStat{User: 125, Idle: 975},
			want: 25,
		},
		"la espera de disco no cuenta como ocupado": {
			prev: gpscpu.TimesStat{User: 100, Idle: 900, Iowait: 50},
			cur:  gpscpu.TimesStat{User: 100, Idle: 900, Iowait: 150},
			want: 0,
		},
		"trabajo de sistema sí cuenta como ocupado": {
			prev: gpscpu.TimesStat{System: 10, Idle: 990},
			cur:  gpscpu.TimesStat{System: 60, Idle: 1040},
			want: 50,
		},
	}
	for name, c := range cases {
		if got := deltaPct(c.prev, c.cur); got != c.want {
			t.Errorf("%s: deltaPct() = %v, se esperaba %v", name, got, c.want)
		}
	}
}

// Contadores que no avanzan o que van hacia atrás (suspensión del equipo,
// migración de la máquina virtual) no deben producir ni una división por cero
// ni un porcentaje absurdo.
func TestDeltaPctGuardsAgainstStalledOrRewoundCounters(t *testing.T) {
	same := gpscpu.TimesStat{User: 100, Idle: 900}
	if got := deltaPct(same, same); got != 0 {
		t.Errorf("contadores sin avanzar: deltaPct() = %v, se esperaba 0", got)
	}

	rewound := gpscpu.TimesStat{User: 10, Idle: 90}
	if got := deltaPct(same, rewound); got != 0 {
		t.Errorf("contadores hacia atrás: deltaPct() = %v, se esperaba 0", got)
	}
}

// El punto de A-06: solo el PRIMER ciclo puede bloquear (cae al muestreo de
// un segundo porque no tiene con qué comparar). A partir del segundo, medir
// la CPU es leer contadores y restar.
func TestCPUCollectorDoesNotBlockAfterTheFirstCycle(t *testing.T) {
	c := NewCPU()
	m := &payload.Metrics{}

	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("primer Collect: %v", err)
	}
	if m.CPU == nil {
		t.Fatal("el primer ciclo debe traer CPU: metrics.cpu es obligatorio en el contrato")
	}

	start := time.Now()
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("segundo Collect: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("el segundo Collect tardó %v; ya no debería muestrear bloqueando", elapsed)
	}
	if m.CPU == nil {
		t.Fatal("m.CPU = nil en el segundo ciclo")
	}
	if len(m.CPU.PerCorePct) == 0 {
		t.Error("PerCorePct vacío: la diferencia entre ciclos no produjo datos")
	}
	for i, v := range m.CPU.PerCorePct {
		if v < 0 || v > 100 {
			t.Errorf("PerCorePct[%d] = %v, fuera de [0,100]", i, v)
		}
	}
}
