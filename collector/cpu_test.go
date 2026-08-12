package collector

import (
	"math"
	"testing"
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
