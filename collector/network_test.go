package collector

import "testing"

func TestRate(t *testing.T) {
	cases := []struct {
		name     string
		cur, old uint64
		elapsed  float64
		want     float64
	}{
		{"incremento normal", 1100, 1000, 10, 10},
		{"sin cambio", 500, 500, 10, 0},
		{"wrap-around: contador reiniciado (interfaz reconectada)", 50, 1000, 10, 5},
		{"elapsed de 1 segundo", 100, 0, 1, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rate(c.cur, c.old, c.elapsed); got != c.want {
				t.Errorf("rate(%d, %d, %v) = %v, se esperaba %v", c.cur, c.old, c.elapsed, got, c.want)
			}
		})
	}
}
