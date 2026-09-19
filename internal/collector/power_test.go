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

// sequencePowerProvider devuelve una lectura distinta en cada llamada, para
// reproducir el arranque de una fuente basada en un contador de energía
// acumulada: el primer ciclo no tiene con qué calcular vatios y el segundo
// sí.
type sequencePowerProvider struct {
	readings []*payload.PowerMetrics
	calls    int
}

func (p *sequencePowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	if p.calls >= len(p.readings) {
		return nil, nil
	}
	m := p.readings[p.calls]
	p.calls++
	return m, nil
}

// El primer ciclo sin dato no autoriza a afirmar nada sobre el hardware: es
// lo que devuelve RAPL siempre, tenga o no sensores la máquina.
func TestPowerCollectorDoesNotWarnOnTheFirstCycle(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	c := &PowerCollector{provider: fakePowerProvider{}, log: log}

	if err := c.Collect(context.Background(), &payload.Metrics{}); err != nil {
		t.Fatalf("Collect() = %v", err)
	}

	if strings.Contains(buf.String(), "no expone ninguna fuente") {
		t.Errorf("el aviso se emitió en el primer ciclo, donde todavía no se sabe nada: %s", buf.String())
	}
}

// El caso que destapó el fallo en campo: una máquina con RAPL legible daba
// nil en el primer ciclo, y el aviso quedaba escrito para siempre aunque
// todos los ciclos siguientes reportasen vatios.
func TestPowerCollectorNeverWarnsWhenTheSecondCycleYieldsWatts(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	provider := &sequencePowerProvider{readings: []*payload.PowerMetrics{
		nil,
		{Watts: 31.5, Estimated: true, Source: "rapl"},
		{Watts: 33.0, Estimated: true, Source: "rapl"},
	}}
	c := &PowerCollector{provider: provider, log: log}

	for range 3 {
		if err := c.Collect(context.Background(), &payload.Metrics{}); err != nil {
			t.Fatalf("Collect() = %v", err)
		}
	}

	if strings.Contains(buf.String(), "no expone ninguna fuente") {
		t.Errorf("se avisó de que no hay fuente en una máquina que sí la tiene: %s", buf.String())
	}
}
