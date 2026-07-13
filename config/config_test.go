package config

import (
	"os"
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
	if cfg.BufferPath != "hygeia-buffer.jsonl" {
		t.Errorf("BufferPath = %q, want %q", cfg.BufferPath, "hygeia-buffer.jsonl")
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

func TestLoad_MissingAgentKey(t *testing.T) {
	t.Setenv("HYGEIA_SERVER_URL", "http://localhost:8080")

	_, err := Load("nonexistent.toml")
	if err == nil {
		t.Fatal("expected error for missing agentKey")
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
