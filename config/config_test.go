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
