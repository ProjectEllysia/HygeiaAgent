package collector

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeHwmonDevice(t *testing.T, base, device, driver string, powerMicroWatts string) {
	t.Helper()
	dir := filepath.Join(base, device)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if driver != "" {
		if err := os.WriteFile(filepath.Join(dir, "name"), []byte(driver), 0o644); err != nil {
			t.Fatalf("WriteFile name: %v", err)
		}
	}
	if powerMicroWatts != "" {
		if err := os.WriteFile(filepath.Join(dir, "power1_input"), []byte(powerMicroWatts), 0o644); err != nil {
			t.Fatalf("WriteFile power1_input: %v", err)
		}
	}
}

func TestHwmonProviderRecognizedDriverConvertsMicrowattsToWatts(t *testing.T) {
	base := t.TempDir()
	fakeHwmonDevice(t, base, "hwmon0", "pmbus", "185500000") // 185.5 W

	p := &hwmonProvider{basePath: base, log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba el sensor reconocido")
	}
	if pw.Watts != 185.5 {
		t.Errorf("Watts = %v, want 185.5", pw.Watts)
	}
	if pw.Estimated {
		t.Error("Estimated = true, un sensor de PSU reconocido es una medición, no una estimación")
	}
	if pw.Source != "hwmon:pmbus" {
		t.Errorf("Source = %q, want %q", pw.Source, "hwmon:pmbus")
	}
}

func TestHwmonProviderUnknownDriverIsIgnoredAndLogged(t *testing.T) {
	base := t.TempDir()
	fakeHwmonDevice(t, base, "hwmon0", "algun-driver-desconocido", "50000000")

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	p := &hwmonProvider{basePath: base, log: log}

	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: el driver no está en la lista reconocida", pw)
	}
	if !strings.Contains(buf.String(), "algun-driver-desconocido") {
		t.Error("el driver desconocido no quedó en el log, y de ahí es de donde debe salir la próxima entrada reconocida (P09)")
	}
}

func TestHwmonProviderIgnoresDevicesWithoutPowerSensor(t *testing.T) {
	base := t.TempDir()
	fakeHwmonDevice(t, base, "hwmon0", "coretemp", "") // solo temperatura, sin power*_input

	p := &hwmonProvider{basePath: base, log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: este hwmon no publica potencia", pw)
	}
}

func TestHwmonProviderAbsentDirectoryReturnsNil(t *testing.T) {
	p := &hwmonProvider{basePath: filepath.Join(t.TempDir(), "no-existe"), log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil", pw)
	}
}

func TestAMDGPUProviderSumsMultipleCards(t *testing.T) {
	base := t.TempDir()
	mkAMDCard := func(card string, microWatts string) {
		dir := filepath.Join(base, card, "device", "hwmon", "hwmon3")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "power1_average"), []byte(microWatts), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	mkAMDCard("card0", "80000000")  // 80 W
	mkAMDCard("card1", "120000000") // 120 W

	p := &amdGPUProvider{basePath: base, log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba la suma de las dos tarjetas")
	}
	if pw.Watts != 200 {
		t.Errorf("Watts = %v, want 200 (80+120)", pw.Watts)
	}
	if pw.Estimated {
		t.Error("Estimated = true, power1_average del driver amdgpu es una medición")
	}
	if pw.Source != "amd_gpu" {
		t.Errorf("Source = %q, want %q", pw.Source, "amd_gpu")
	}
}

func TestAMDGPUProviderNoCardsReturnsNil(t *testing.T) {
	p := &amdGPUProvider{basePath: t.TempDir(), log: discardLogger()}
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: sin GPU AMD", pw)
	}
}
