package main

import (
	"strings"
	"testing"
)

func TestAgentVersion_IsSemver(t *testing.T) {
	if AgentVersion == "" {
		t.Fatal("AgentVersion should not be empty")
	}
	if !strings.Contains(AgentVersion, ".") {
		t.Errorf("AgentVersion = %q, expected semver format (x.y.z)", AgentVersion)
	}
}

func TestAgentVersion_IsDev(t *testing.T) {
	if !strings.HasSuffix(AgentVersion, "-dev") {
		t.Errorf("AgentVersion = %q, expected dev suffix", AgentVersion)
	}
}
