package collector

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeRAPLDomain crea un directorio de dominio con energy_uj y, si maxRange
// > 0, max_energy_range_uj — la misma pareja de ficheros que expone
// powercap.
func fakeRAPLDomain(t *testing.T, base, name string, energyUJ, maxRange uint64) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "energy_uj"), []byte(strconv.FormatUint(energyUJ, 10)), 0o644); err != nil {
		t.Fatalf("WriteFile energy_uj: %v", err)
	}
	if maxRange > 0 {
		if err := os.WriteFile(filepath.Join(dir, "max_energy_range_uj"), []byte(strconv.FormatUint(maxRange, 10)), 0o644); err != nil {
			t.Fatalf("WriteFile max_energy_range_uj: %v", err)
		}
	}
}

func setEnergy(t *testing.T, base, name string, energyUJ uint64) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, name, "energy_uj"), []byte(strconv.FormatUint(energyUJ, 10)), 0o644); err != nil {
		t.Fatalf("WriteFile energy_uj: %v", err)
	}
}

func TestRAPLProviderFirstCycleReturnsNilWithoutError(t *testing.T) {
	base := t.TempDir()
	fakeRAPLDomain(t, base, "intel-rapl:0", 1_000_000, 0)
	p := &raplProvider{basePath: base, log: discardLogger()}

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil en el primer ciclo (no hay muestra anterior)", pw)
	}
}

func TestRAPLProviderDerivesWattsFromEnergyDelta(t *testing.T) {
	base := t.TempDir()
	fakeRAPLDomain(t, base, "intel-rapl:0", 1_000_000, 0)
	p := &raplProvider{basePath: base, log: discardLogger()}

	if _, err := p.Read(context.Background()); err != nil {
		t.Fatalf("primer Read() err = %v", err)
	}

	// Segunda lectura 2 segundos "después": +20 J = 20.000.000 µJ -> 10 W.
	p.prevTime = time.Now().Add(-2 * time.Second)
	setEnergy(t, base, "intel-rapl:0", 21_000_000)

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("segundo Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba un valor tras dos lecturas con muestra anterior")
	}
	if diff := pw.Watts - 10.0; diff > 0.5 || diff < -0.5 {
		t.Errorf("Watts = %v, se esperaba ~10", pw.Watts)
	}
	if !pw.Estimated {
		t.Error("Estimated = false, RAPL siempre es una aproximación (package de CPU, no la máquina entera)")
	}
	if pw.Source != "rapl" {
		t.Errorf("Source = %q, want %q", pw.Source, "rapl")
	}
}

// TestRAPLProviderHandlesCounterWraparound es el criterio de cierre de P07:
// el contador da la vuelta a 0 al llegar a max_energy_range_uj, y sin
// corregirlo la resta ingenua saldría negativa.
func TestRAPLProviderHandlesCounterWraparound(t *testing.T) {
	base := t.TempDir()
	const maxRange = uint64(1_000_000) // rango minúsculo para forzar el wraparound en el test
	fakeRAPLDomain(t, base, "intel-rapl:0", 900_000, maxRange)
	p := &raplProvider{basePath: base, log: discardLogger()}

	if _, err := p.Read(context.Background()); err != nil {
		t.Fatalf("primer Read() err = %v", err)
	}

	// El contador dio la vuelta: de 900.000 pasó a 100.000 sin llegar a
	// negativo, es decir, recorrió (1.000.000 - 900.000) + 100.000 = 200.000 µJ.
	p.prevTime = time.Now().Add(-1 * time.Second)
	setEnergy(t, base, "intel-rapl:0", 100_000)

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("segundo Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba el vatio corregido por el wraparound")
	}
	wantWatts := 200_000.0 / 1e6 / 1.0 // 0.2 W
	if diff := pw.Watts - wantWatts; diff > 0.01 || diff < -0.01 {
		t.Errorf("Watts = %v, se esperaba %v (corregido con max_energy_range_uj), NUNCA negativo", pw.Watts, wantWatts)
	}
}

// TestRAPLProviderDiscardsWraparoundWithoutMaxRange es la otra mitad de P07:
// sin max_energy_range_uj no hay con qué corregir el rango, y la política
// segura es descartar el intervalo en vez de adivinar.
func TestRAPLProviderDiscardsWraparoundWithoutMaxRange(t *testing.T) {
	base := t.TempDir()
	fakeRAPLDomain(t, base, "intel-rapl:0", 900_000, 0) // sin max_energy_range_uj
	p := &raplProvider{basePath: base, log: discardLogger()}

	if _, err := p.Read(context.Background()); err != nil {
		t.Fatalf("primer Read() err = %v", err)
	}

	p.prevTime = time.Now().Add(-1 * time.Second)
	setEnergy(t, base, "intel-rapl:0", 100_000) // dio la vuelta

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: el wraparound sin max_energy_range_uj se descarta, no se adivina", pw)
	}
}

func TestRAPLProviderSubdomainsExcludedFromDiscovery(t *testing.T) {
	base := t.TempDir()
	fakeRAPLDomain(t, base, "intel-rapl:0", 1_000_000, 0)
	fakeRAPLDomain(t, base, "intel-rapl:0:0", 500_000, 0) // subdominio: NO debe sumarse aparte
	fakeRAPLDomain(t, base, "intel-rapl:1", 2_000_000, 0)

	domains := discoverRAPLDomains(base)
	if len(domains) != 2 {
		t.Fatalf("discoverRAPLDomains() = %v, se esperaban 2 dominios de primer nivel (no el subdominio)", domains)
	}
}

// TestRAPLProviderPermissionDeniedDegradesSilently es el criterio de cierre
// de P08: energy_uj es 0400 desde Linux 5.10, y un agente sin privilegio
// tiene que degradar a "sin fuente" sin romper el heartbeat — igual que la
// ausencia total, pero con un mensaje distinto (verificado por separado, ver
// TestDiagnosePowerReportsPermissionDenied en power_linux_test.go).
func TestRAPLProviderPermissionDeniedDegradesSilently(t *testing.T) {
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

	p := &raplProvider{basePath: base, log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v, se esperaba nil (degradación silenciosa)", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: sin privilegio para leer energy_uj", pw)
	}
}

func TestRAPLProviderAbsentReturnsNilWithoutError(t *testing.T) {
	p := &raplProvider{basePath: filepath.Join(t.TempDir(), "no-existe"), log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v, se esperaba nil (ausencia no es un fallo)", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: esta máquina no expone powercap", pw)
	}
}
