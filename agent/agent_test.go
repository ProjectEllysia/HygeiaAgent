package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

const testKey = "abcd1234.0123456789abcdef"

// newTestAgent construye un agente sobre un directorio temporal, con o sin
// clave, para no tocar la config real de la máquina.
func newTestAgent(t *testing.T, agentKey string) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	body := "serverUrl = \"https://ellysia.example/hygeia\"\n"
	if agentKey != "" {
		body += "agentKey = \"" + agentKey + "\"\n"
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HYGEIA_DATA_DIR", dir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(log, cfg), cfgPath
}

// Sin clave el agente arranca igualmente, en estado "sin configurar" (§11.3):
// si crasheara, el tray nunca podría configurarlo.
func TestNewWithoutKeyStartsUnconfigured(t *testing.T) {
	a, _ := newTestAgent(t, "")
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("State = %q, se esperaba %q", got, control.StateUnconfigured)
	}
}

// Con clave, el agente arranca en "iniciando": todavía no ha hablado con el
// backend, así que afirmar "conectado" sería falso.
func TestNewWithKeyStartsStarting(t *testing.T) {
	a, _ := newTestAgent(t, testKey)
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("State = %q, se esperaba %q", got, control.StateStarting)
	}
}

// Enroll persiste la clave en disco: al recargar la config debe seguir ahí,
// o el agente volvería a arrancar sin configurar tras un reinicio.
func TestEnrollPersistsKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, "")

	if err := a.Enroll(testKey); err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("tras Enroll, State = %q, se esperaba %q", got, control.StateStarting)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("recargando config: %v", err)
	}
	if reloaded.AgentKey != testKey {
		t.Errorf("clave persistida = %q, se esperaba %q", reloaded.AgentKey, testKey)
	}
}

// §11.7: un agente ya dado de alta no se puede reapuntar desde el canal de
// control. Si se pudiera, un usuario local sin privilegios podría pisar la
// clave de un activo o mandar sus métricas a otro servidor.
func TestEnrollRejectedWhenAlreadyConfigured(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	const otherKey = "wxyz9999.fedcba9876543210"
	err := a.Enroll(otherKey)
	if err == nil {
		t.Fatal("Enroll() sobre agente configurado = nil, se esperaba rechazo")
	}
	if !strings.Contains(err.Error(), "ya está dado de alta") {
		t.Errorf("error = %q, no explica el motivo del rechazo", err)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != testKey {
		t.Errorf("la clave original fue pisada: %q", reloaded.AgentKey)
	}
}

func TestEnrollRejectsMalformedKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, "")

	if err := a.Enroll("no-es-una-clave"); err == nil {
		t.Fatal("Enroll() con clave malformada = nil, se esperaba error")
	}
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("State = %q tras enrollment fallido, se esperaba %q", got, control.StateUnconfigured)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != "" {
		t.Errorf("se persistió una clave malformada: %q", reloaded.AgentKey)
	}
}

// Reset borra la clave y persiste el cambio: al recargar la config no debe
// quedar rastro de ella (§5 — nunca en logs, y tampoco superviviente en
// disco tras un reset explícito).
func TestResetClearsKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	if err := a.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("tras Reset, State = %q, se esperaba %q", got, control.StateUnconfigured)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("recargando config: %v", err)
	}
	if reloaded.AgentKey != "" {
		t.Errorf("clave tras reset = %q, se esperaba vacía", reloaded.AgentKey)
	}
}

// Tras Reset, el agente vuelve a aceptar un Enroll (es justo el punto:
// habilitar volver a dar de alta sin tocar la config a mano).
func TestEnrollAfterReset(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	if err := a.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	const newKey = "wxyz9999.fedcba9876543210"
	if err := a.Enroll(newKey); err != nil {
		t.Fatalf("Enroll() tras Reset, error = %v", err)
	}
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("tras Enroll post-reset, State = %q, se esperaba %q", got, control.StateStarting)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != newKey {
		t.Errorf("clave persistida = %q, se esperaba %q", reloaded.AgentKey, newKey)
	}
}

// Simétrico a TestEnrollRejectedWhenAlreadyConfigured: no tiene sentido
// resetear un agente que ya está sin configurar, y silenciarlo escondería
// un bug de estado en el caller (tray o test).
func TestResetRejectedWhenUnconfigured(t *testing.T) {
	a, _ := newTestAgent(t, "")

	err := a.Reset()
	if err == nil {
		t.Fatal("Reset() sobre agente sin configurar = nil, se esperaba rechazo")
	}
	if !strings.Contains(err.Error(), "ya está sin configurar") {
		t.Errorf("error = %q, no explica el motivo del rechazo", err)
	}
}

// time.Time serializa con precisión de nanosegundo variable (a veces 7-9
// dígitos decimales) — el backend rechazaba con 422 cualquier collectedAt
// con más de 6, sin relación con la validez del dato (bug real detectado en
// producción, no reproducible de forma determinista sin este truncado).
func TestCollectPayloadTruncatesCollectedAtToMicroseconds(t *testing.T) {
	a, _ := newTestAgent(t, "")
	p := a.collectPayload(context.Background())

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// El grupo de fracción es opcional: si el truncado cae justo en un
	// microsegundo múltiplo de 10^6 (raro, pero posible), el segundo sale
	// sin punto decimal — eso también es válido, no un fallo del test.
	re := regexp.MustCompile(`"collectedAt":"[^".]*(\.(\d+))?Z"`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("no se encontró collectedAt en el payload")
	}
	if len(m[2]) > 6 {
		t.Errorf("collectedAt tiene %d dígitos decimales, se esperaban ≤6: %s", len(m[2]), m[2])
	}
}

// drainBuffer debe descartar (no reencolar) un payload que el backend
// rechaza de forma permanente — si no, queda dando vueltas en el buffer sin
// poder entregarse nunca (bug real: payloads de horas de antigüedad
// atascados en producción, ver shipper.PermanentError).
func TestDrainBufferDropsPermanentlyRejectedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	body := "serverUrl = \"" + srv.URL + "\"\nagentKey = \"" + testKey + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYGEIA_DATA_DIR", dir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(log, cfg)

	if err := a.buf.Push(&payload.Payload{AgentVersion: "test"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := a.buf.Len(); got != 1 {
		t.Fatalf("buf.Len() antes de drenar = %d, se esperaba 1", got)
	}

	a.drainBuffer(context.Background(), a.shp)

	if got := a.buf.Len(); got != 0 {
		t.Errorf("buf.Len() tras drenar un rechazo permanente = %d, se esperaba 0 (descartado, no reencolado)", got)
	}
}
