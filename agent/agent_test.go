package agent

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/control"
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
