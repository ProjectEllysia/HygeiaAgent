package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// TestLinuxPowerProviderAggregationPolicy es el criterio de cierre de P12:
// una fuente de sistema completo gana sola, las de componente se suman entre
// sí, y sin ninguna sale nil. Se prueban las cuatro combinaciones del issue.
func TestLinuxPowerProviderAggregationPolicy(t *testing.T) {
	rapl := fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 40, Source: "rapl"}}
	nvidia := fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 60, Source: "nvidia"}}
	amd := fakePowerProvider{}
	psu := fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 300, Estimated: false, Source: "hwmon:pmbus"}}
	none := fakePowerProvider{}

	cases := []struct {
		name       string
		hwmon      PowerProvider
		rapl       PowerProvider
		nvidia     PowerProvider
		amd        PowerProvider
		wantNil    bool
		wantWatts  float64
		wantSource string
	}{
		{
			name: "solo RAPL", hwmon: none, rapl: rapl, nvidia: none, amd: none,
			wantWatts: 40, wantSource: "rapl",
		},
		{
			name: "RAPL + NVIDIA suman", hwmon: none, rapl: rapl, nvidia: nvidia, amd: none,
			wantWatts: 100, wantSource: "rapl+nvidia",
		},
		{
			name: "PSU gana sola frente a RAPL", hwmon: psu, rapl: rapl, nvidia: none, amd: none,
			wantWatts: 300, wantSource: "hwmon:pmbus",
		},
		{
			name: "ninguna fuente", hwmon: none, rapl: none, nvidia: none, amd: amd,
			wantNil: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &linuxPowerProvider{hwmon: c.hwmon, rapl: c.rapl, nvidia: c.nvidia, amd: c.amd}
			pw, err := p.Read(context.Background())
			if err != nil {
				t.Fatalf("Read() err = %v", err)
			}
			if c.wantNil {
				if pw != nil {
					t.Errorf("Read() = %+v, se esperaba nil", pw)
				}
				return
			}
			if pw == nil {
				t.Fatal("Read() = nil, se esperaba un valor")
			}
			if pw.Watts != c.wantWatts {
				t.Errorf("Watts = %v, want %v", pw.Watts, c.wantWatts)
			}
			if pw.Source != c.wantSource {
				t.Errorf("Source = %q, want %q", pw.Source, c.wantSource)
			}
		})
	}
}

func TestDiagnosePowerReportsPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("corriendo como root: chmod 0000 no produce EACCES")
	}
	base := t.TempDir()
	fakeRAPLDomain(t, base, "intel-rapl:0", 1_000_000, 0)
	energyFile := filepath.Join(base, "intel-rapl:0", "energy_uj")
	if err := os.Chmod(energyFile, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(energyFile, 0o644) })

	restore := defaultRAPLBasePath
	defaultRAPLBasePath = base
	t.Cleanup(func() { defaultRAPLBasePath = restore })

	d := diagnosePower()
	if d.Available {
		t.Errorf("Available = true, se esperaba false (sin privilegio)")
	}
	if !strings.Contains(d.Hint, "root") {
		t.Errorf("Hint = %q, se esperaba que mencionara ejecutar como root", d.Hint)
	}
}

func TestDiagnosePowerReportsAbsentWhenNothingFound(t *testing.T) {
	restoreRAPL, restoreHwmon, restoreDRM := defaultRAPLBasePath, defaultHwmonBasePath, defaultDRMBasePath
	empty := t.TempDir()
	defaultRAPLBasePath, defaultHwmonBasePath, defaultDRMBasePath = empty, empty, empty
	t.Cleanup(func() {
		defaultRAPLBasePath, defaultHwmonBasePath, defaultDRMBasePath = restoreRAPL, restoreHwmon, restoreDRM
	})

	d := diagnosePower()
	if d.Available {
		t.Errorf("Available = true, se esperaba false: no hay ninguna fuente en el árbol vacío")
	}
	if d.Hint != "" {
		t.Errorf("Hint = %q, se esperaba vacío: la ausencia sin más no sugiere ninguna acción", d.Hint)
	}
}
