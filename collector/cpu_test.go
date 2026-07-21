package collector

import (
	"math"
	"testing"
)

func TestRound1(t *testing.T) {
	if got := round1(87.549); got != 87.5 {
		t.Errorf("round1(87.549) = %v, se esperaba 87.5", got)
	}
	if got := round1(0); got != 0 {
		t.Errorf("round1(0) = %v, se esperaba 0", got)
	}
}

// El cast a int64 de un valor no finito es indefinido en Go — round1 debe
// devolver 0 en vez de propagar basura al payload (plan §12.2, Tier 2).
func TestRound1GuardsAgainstNonFiniteInput(t *testing.T) {
	cases := map[string]float64{
		"NaN":  math.NaN(),
		"+Inf": math.Inf(1),
		"-Inf": math.Inf(-1),
	}
	for name, in := range cases {
		if got := round1(in); got != 0 {
			t.Errorf("round1(%s) = %v, se esperaba 0", name, got)
		}
	}
}
