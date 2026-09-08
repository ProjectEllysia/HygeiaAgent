package collector

import (
	"context"
	"errors"
	"testing"
)

// defaultCPUModelForTest sustituye defaultCPUModel (usado por diagnosePower)
// y devuelve una función para restaurarlo: diagnosePower no acepta un
// windowsPowerProvider de doble, así que necesita este seam para no depender
// de la CPU real de la máquina que ejecuta la suite.
func defaultCPUModelForTest(t *testing.T, fn func() (string, error)) func() {
	t.Helper()
	original := defaultCPUModel
	defaultCPUModel = fn
	return func() { defaultCPUModel = original }
}

func TestTdpForModelMatchesKnownFamilies(t *testing.T) {
	cases := []struct {
		model   string
		wantTDP float64
		wantOK  bool
	}{
		{"13th Gen Intel(R) Core(TM) i9-13900K", 125, true},
		{"Intel(R) Core(TM) i7-9700 CPU @ 3.00GHz", 65, true},
		{"AMD Ryzen 9 5900X 12-Core Processor", 105, true},
		{"AMD Ryzen 5 5600G with Radeon Graphics", 65, true},
		{"Intel(R) Xeon(R) CPU E5-2670 v3 @ 2.30GHz", 150, true},
		{"AMD EPYC 7502 32-Core Processor", 225, true},
		{"Un procesador experimental que no está en la tabla", 0, false},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			tdp, ok := tdpForModel(c.model)
			if ok != c.wantOK {
				t.Fatalf("tdpForModel(%q) ok = %v, want %v", c.model, ok, c.wantOK)
			}
			if ok && tdp != c.wantTDP {
				t.Errorf("tdpForModel(%q) tdp = %v, want %v", c.model, tdp, c.wantTDP)
			}
		})
	}
}

func TestEstimateWattsFromUtilizationRange(t *testing.T) {
	const tdp = 65.0
	rest := estimateWattsFromUtilization(tdp, 0)
	medium := estimateWattsFromUtilization(tdp, 50)
	full := estimateWattsFromUtilization(tdp, 100)

	if rest != idleBaseWatts {
		t.Errorf("reposo = %v, want %v (solo la base)", rest, idleBaseWatts)
	}
	if medium <= rest || medium >= full {
		t.Errorf("media carga = %v, se esperaba entre reposo (%v) y plena carga (%v)", medium, rest, full)
	}
	if want := idleBaseWatts + tdp; full != want {
		t.Errorf("plena carga = %v, want %v (base + TDP completo)", full, want)
	}
}

func TestEstimateWattsFromUtilizationClampsOutOfRange(t *testing.T) {
	if got := estimateWattsFromUtilization(65, -10); got != idleBaseWatts {
		t.Errorf("utilización negativa: got %v, want %v", got, idleBaseWatts)
	}
	if got, want := estimateWattsFromUtilization(65, 150), idleBaseWatts+65; got != want {
		t.Errorf("utilización > 100: got %v, want %v", got, want)
	}
}

func TestWindowsPowerProviderAlwaysEstimatedWithModelSource(t *testing.T) {
	p := &windowsPowerProvider{
		log:      discardLogger(),
		gpu:      newNVIDIAGPUProvider(discardLogger()),
		cpuModel: func() (string, error) { return "Intel(R) Core(TM) i5-10400", nil },
		cpuUsage: func(ctx context.Context) (float64, error) { return 40, nil },
	}
	// Sin nvidia-smi instalado en el entorno de test: el gpu real no aporta
	// nada, así que solo se comprueba el camino de CPU.
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba una estimación")
	}
	if !pw.Estimated {
		t.Error("Estimated = false, el modelo de Windows SIEMPRE es una estimación")
	}
	if pw.Source != "model" {
		t.Errorf("Source = %q, want %q (sin GPU NVIDIA en este test)", pw.Source, "model")
	}
}

func TestWindowsPowerProviderAddsNVIDIAGPU(t *testing.T) {
	p := &windowsPowerProvider{
		log:      discardLogger(),
		cpuModel: func() (string, error) { return "Intel(R) Core(TM) i5-10400", nil },
		cpuUsage: func(ctx context.Context) (float64, error) { return 0, nil },
	}
	p.gpu = &nvidiaGPUProvider{
		log:      discardLogger(),
		lookPath: func(string) (string, error) { return "/usr/bin/nvidia-smi", nil },
		run:      func(ctx context.Context, bin string) ([]byte, error) { return []byte("75.0\n"), nil },
	}

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil")
	}
	if pw.Source != "model+nvidia" {
		t.Errorf("Source = %q, want %q", pw.Source, "model+nvidia")
	}
	if want := idleBaseWatts + 75.0; pw.Watts != want {
		t.Errorf("Watts = %v, want %v (base de reposo + 75 W de GPU)", pw.Watts, want)
	}
}

// TestWindowsPowerProviderUnknownCPUDegradesToNil es el criterio de cierre
// de P13: una CPU que no está en la tabla no debe estimar con un TDP
// inventado.
func TestWindowsPowerProviderUnknownCPUDegradesToNil(t *testing.T) {
	p := &windowsPowerProvider{
		log:      discardLogger(),
		gpu:      newNVIDIAGPUProvider(discardLogger()),
		cpuModel: func() (string, error) { return "CPU Experimental No Catalogada 9000", nil },
		cpuUsage: func(ctx context.Context) (float64, error) { return 50, nil },
	}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: CPU no reconocida en la tabla de TDP", pw)
	}
}

func TestWindowsPowerProviderCPUModelErrorDegradesToNil(t *testing.T) {
	p := &windowsPowerProvider{
		log:      discardLogger(),
		gpu:      newNVIDIAGPUProvider(discardLogger()),
		cpuModel: func() (string, error) { return "", errors.New("wmi no disponible") },
		cpuUsage: func(ctx context.Context) (float64, error) { return 50, nil },
	}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v, se esperaba degradación silenciosa", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil", pw)
	}
}

func TestDiagnosePowerWindowsRecognizedCPU(t *testing.T) {
	restore := defaultCPUModelForTest(t, func() (string, error) { return "Intel(R) Core(TM) i7-9700", nil })
	defer restore()

	d := diagnosePower()
	if !d.Available {
		t.Errorf("Available = false, se esperaba true (CPU en la tabla)")
	}
	if d.Hint == "" {
		t.Error("Hint vacío: siempre debe recordar que no es una medición real")
	}
}

func TestDiagnosePowerWindowsUnknownCPU(t *testing.T) {
	restore := defaultCPUModelForTest(t, func() (string, error) { return "CPU no catalogada", nil })
	defer restore()

	d := diagnosePower()
	if d.Available {
		t.Error("Available = true, se esperaba false: CPU no reconocida")
	}
}
