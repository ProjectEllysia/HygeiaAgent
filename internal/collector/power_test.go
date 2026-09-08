package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
	c := &PowerCollector{provider: fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 42.5, Source: "rapl"}}, log: discardLogger()}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Fatalf("Collect() = %v", err)
	}
	if m.Power == nil || m.Power.Watts != 42.5 {
		t.Errorf("Power = %+v, se esperaba watts:42.5", m.Power)
	}
}

func TestPowerCollectorZeroWattsIsNotDiscarded(t *testing.T) {
	c := &PowerCollector{provider: fakePowerProvider{metrics: &payload.PowerMetrics{Watts: 0, Source: "rapl"}}, log: discardLogger()}
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
	c := &PowerCollector{provider: fakePowerProvider{metrics: nil, err: nil}, log: discardLogger()}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Fatalf("Collect() = %v, se esperaba nil (sin fuente no es un fallo)", err)
	}
	if m.Power != nil {
		t.Errorf("Power = %+v, se esperaba nil (esta máquina no tiene fuente de potencia)", m.Power)
	}
}

// TestPowerCollectorProviderErrorNeverBreaksTheHeartbeat es el criterio de
// cierre de P04: un proveedor que falla no debe propagar el error (eso
// descartaría el heartbeat entero en el peor momento posible, cuando más
// falta hace seguir reportando CPU y memoria).
func TestPowerCollectorProviderErrorNeverBreaksTheHeartbeat(t *testing.T) {
	c := &PowerCollector{provider: fakePowerProvider{err: errors.New("fallo leyendo el sensor")}, log: discardLogger()}
	var m payload.Metrics
	if err := c.Collect(context.Background(), &m); err != nil {
		t.Errorf("Collect() = %v, un fallo del proveedor no debe romper el heartbeat", err)
	}
	if m.Power != nil {
		t.Errorf("Power = %+v, se esperaba nil tras un error del proveedor", m.Power)
	}
}

func TestPowerCollectorWarnsOnlyOnceAcrossCycles(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	c := &PowerCollector{provider: fakePowerProvider{err: errors.New("fallo leyendo el sensor")}, log: log}

	for range 5 {
		if err := c.Collect(context.Background(), &payload.Metrics{}); err != nil {
			t.Fatalf("Collect() = %v", err)
		}
	}

	if got := strings.Count(buf.String(), "consumo eléctrico no disponible"); got != 1 {
		t.Errorf("el aviso apareció %d veces en 5 ciclos, se esperaba exactamente 1: %s", got, buf.String())
	}
}

func TestPowerCollectorNoSourceWarnsOnlyOnceAcrossCycles(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	c := &PowerCollector{provider: fakePowerProvider{}, log: log}

	for range 5 {
		if err := c.Collect(context.Background(), &payload.Metrics{}); err != nil {
			t.Fatalf("Collect() = %v", err)
		}
	}

	if got := strings.Count(buf.String(), "no expone ninguna fuente"); got != 1 {
		t.Errorf("el aviso apareció %d veces en 5 ciclos, se esperaba exactamente 1: %s", got, buf.String())
	}
}

func TestPowerCollectorName(t *testing.T) {
	if got := NewPower(discardLogger()).Name(); got != "power" {
		t.Errorf("Name() = %q, want %q", got, "power")
	}
}
