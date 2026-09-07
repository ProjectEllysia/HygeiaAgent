package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// fakePowerProvider es el doble de test: cada caso fija qué devuelve Read.
type fakePowerProvider struct {
	metrics *payload.PowerMetrics
	err     error
}

func (f fakePowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	return f.metrics, f.err
}

func TestPowerCollectorPositiveValueReachesPayload(t *testing.T) {
	c := &PowerCollector{provider: fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 42.5, Source: "rapl"}}}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Power == nil || m.Power.Watts != 42.5 {
		t.Errorf("Power = %+v, se esperaba watts:42.5", m.Power)
	}
}

func TestPowerCollectorZeroWattsIsNotDiscarded(t *testing.T) {
	c := &PowerCollector{provider: fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 0, Source: "rapl"}}}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Power == nil {
		t.Fatal("Power = nil, un valor de 0 W debe llegar al payload (es distinto de \"sin fuente\")")
	}
	if m.Power.Watts != 0 {
		t.Errorf("Power.Watts = %f, want 0", m.Power.Watts)
	}
}

func TestPowerCollectorNilWithoutErrorOmitsPower(t *testing.T) {
	c := &PowerCollector{provider: fakePowerProvider{metrics: nil, err: nil}}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Fatalf("Collect() = %v, se esperaba nil (sin fuente no es un fallo)", err)
	}
	if m.Power != nil {
		t.Errorf("Power = %+v, se esperaba nil (esta máquina no tiene fuente de potencia)", m.Power)
	}
}

func TestPowerCollectorProviderErrorPropagates(t *testing.T) {
	wantErr := errors.New("fallo leyendo el sensor")
	c := &PowerCollector{provider: fakePowerProvider{err: wantErr}}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); !errors.Is(err, wantErr) {
		t.Errorf("Collect() = %v, want %v", err, wantErr)
	}
	if m.Power != nil {
		t.Errorf("Power = %+v, se esperaba nil tras un error del proveedor", m.Power)
	}
}

func TestPowerCollectorName(t *testing.T) {
	if got := NewPower().Name(); got != "power" {
		t.Errorf("Name() = %q, want %q", got, "power")
	}
}
