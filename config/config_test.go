package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.ServerURL != "http://localhost:8080" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "http://localhost:8080")
	}
	if cfg.AgentKey != "test-key" {
		t.Errorf("AgentKey = %q, want %q", cfg.AgentKey, "test-key")
	}
	if cfg.IntervalSec != 15 {
		t.Errorf("IntervalSec = %d, want %d", cfg.IntervalSec, 15)
	}
	// El default dejó de ser un fichero relativo al working directory: el
	// servicio puede arrancar con un cwd que no controla (p. ej. System32
	// en Windows), así que vive en DataDir() junto al resto del estado.
	if want := filepath.Join(DataDir(), "buffer.jsonl"); cfg.BufferPath != want {
		t.Errorf("BufferPath = %q, want %q", cfg.BufferPath, want)
	}
	if len(cfg.Collectors) != 5 {
		t.Errorf("len(Collectors) = %d, want %d", len(cfg.Collectors), 5)
	}
	if cfg.InventoryIntervalSec != 21600 {
		t.Errorf("InventoryIntervalSec = %d, want %d", cfg.InventoryIntervalSec, 21600)
	}
	if cfg.BufferMaxItems != defaultBufferMaxItems {
		t.Errorf("BufferMaxItems = %d, want %d", cfg.BufferMaxItems, defaultBufferMaxItems)
	}
}

func TestLoad_BufferMaxItemsEnvOverride(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_BUFFER_MAX_ITEMS", "250")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.BufferMaxItems != 250 {
		t.Errorf("BufferMaxItems = %d, want %d", cfg.BufferMaxItems, 250)
	}
}

// Cero o negativo significan "no lo he configurado", no "buffer desactivado":
// un buffer de tamaño cero descartaría en silencio todo lo que no se pueda
// enviar, que es justo lo contrario de lo que el buffer existe para evitar.
func TestLoad_BufferMaxItemsNonPositiveFallsBackToDefault(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")

	for _, v := range []string{"0", "-5"} {
		t.Setenv("HYGEIA_BUFFER_MAX_ITEMS", v)
		cfg, err := Load("nonexistent.toml")
		if err != nil {
			t.Fatalf("Load() con %q = %v", v, err)
		}
		if cfg.BufferMaxItems != defaultBufferMaxItems {
			t.Errorf("BufferMaxItems con %q = %d, want %d", v, cfg.BufferMaxItems, defaultBufferMaxItems)
		}
	}
}

// Un typo no debe poder llenar el disco del activo justo mientras el backend
// está caído y nadie está mirando.
func TestLoad_BufferMaxItemsAboveCeilingIsClamped(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_BUFFER_MAX_ITEMS", "10000000")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.BufferMaxItems != maxBufferMaxItems {
		t.Errorf("BufferMaxItems = %d, want %d (techo)", cfg.BufferMaxItems, maxBufferMaxItems)
	}
}

func TestLoad_InventoryIntervalEnvOverride(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_INVENTORY_INTERVAL_SEC", "3600")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.InventoryIntervalSec != 3600 {
		t.Errorf("InventoryIntervalSec = %d, want %d", cfg.InventoryIntervalSec, 3600)
	}
}

// Un valor por debajo del piso no debería dejar el escaneo de inventario
// corriendo prácticamente en cada tick por un typo en config/env.
func TestLoad_InventoryIntervalBelowFloorIsClamped(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_INVENTORY_INTERVAL_SEC", "10")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.InventoryIntervalSec != minInventoryIntervalSec {
		t.Errorf("InventoryIntervalSec = %d, want %d (piso)", cfg.InventoryIntervalSec, minInventoryIntervalSec)
	}
}

// Un valor absurdamente alto no debería dejar el inventario sin refrescarse
// durante meses/años por un typo (mismo espíritu que el tope de IntervalSec).
func TestLoad_InventoryIntervalAboveCeilingIsClamped(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_INVENTORY_INTERVAL_SEC", "99999999")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.InventoryIntervalSec != maxInventoryIntervalSec {
		t.Errorf("InventoryIntervalSec = %d, want %d (tope)", cfg.InventoryIntervalSec, maxInventoryIntervalSec)
	}
}

func TestLoad_MissingServerURL(t *testing.T) {
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")

	_, err := Load("nonexistent.toml")
	if err == nil {
		t.Fatal("expected error for missing serverUrl")
	}
}

// Sin agentKey, Load NO debe fallar (§11.3): el servicio arranca en estado
// "sin configurar" y espera el enrollment del tray por el canal de control.
// Esta prueba antes esperaba justo lo contrario (error), de cuando
// agentKey todavía era obligatorio — quedó obsoleta al introducir el
// enrollment sin clave y el merge con esa rama la arrastró sin actualizar.
func TestLoad_MissingAgentKeyIsNotAnError(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v, se esperaba nil (agentKey es opcional)", err)
	}
	if cfg.IsConfigured() {
		t.Error("IsConfigured() = true sin agentKey, se esperaba false")
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://override:9090")
	t.Setenv("HYGEIA_AGENT_KEY", "override-key")
	t.Setenv("HYGEIA_INTERVAL_SEC", "30")
	t.Setenv("HYGEIA_BUFFER_PATH", "/tmp/buffer.jsonl")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.ServerURL != "http://override:9090" {
		t.Errorf("ServerURL = %q", "http://override:9090")
	}
	if cfg.IntervalSec != 30 {
		t.Errorf("IntervalSec = %d, want %d", cfg.IntervalSec, 30)
	}
	if cfg.BufferPath != "/tmp/buffer.jsonl" {
		t.Errorf("BufferPath = %q, want %q", cfg.BufferPath, "/tmp/buffer.jsonl")
	}
}

func TestLoad_InvalidIntervalEnv(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")
	t.Setenv("HYGEIA_INTERVAL_SEC", "not-a-number")

	_, err := Load("nonexistent.toml")
	if err == nil {
		t.Fatal("expected error for invalid interval")
	}
}

func TestApplyEnv_NoEnvVars(t *testing.T) {
	c := &Config{
		ServerURL: "http://default",
		AgentKey:  "default-key",
	}
	if err := applyEnv(c); err != nil {
		t.Fatalf("applyEnv() = %v", err)
	}
	if c.ServerURL != "http://default" {
		t.Errorf("ServerURL changed to %q", c.ServerURL)
	}
}

func TestLoad_EnvRemovesLeadingTrailingWhitespace(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "  http://localhost:8080  ")
	t.Setenv("HYGEIA_AGENT_KEY", "test-key")

	cfg, err := Load("nonexistent.toml")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.ServerURL != "  http://localhost:8080  " {
		t.Errorf("expected whitespace to be preserved, got %q", cfg.ServerURL)
	}
}

func TestLoad_WithTOMLFile(t *testing.T) {
	content := []byte(`
serverUrl = "http://toml:8080"
agentKey = "toml-key"
intervalSec = 60
bufferPath = "/tmp/toml-buffer.jsonl"
collectors = ["cpu", "memory"]
`)
	tmpFile, err := os.CreateTemp(t.TempDir(), "config-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(content); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HYGEIA_SERVER_URL", "http://toml:8080")
	t.Setenv("HYGEIA_AGENT_KEY", "toml-key")

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.ServerURL != "http://toml:8080" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "http://toml:8080")
	}
}
