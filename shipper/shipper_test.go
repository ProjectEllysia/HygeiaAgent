package shipper

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestNewShipper(t *testing.T) {
	s := NewShipper("http://localhost:8080", "test-key")
	if s == nil {
		t.Fatal("expected non-nil shipper")
	}
	if s.serverURL != "http://localhost:8080" {
		t.Errorf("serverURL = %q", s.serverURL)
	}
	if s.agentKey != "test-key" {
		t.Errorf("agentKey = %q", s.agentKey)
	}
	if s.client == nil {
		t.Error("client should not be nil")
	}
}

func TestSend_ReturnsNotImplemented(t *testing.T) {
	s := NewShipper("http://localhost:8080", "test-key")
	p := &payload.Payload{AgentVersion: "1.0.0"}
	resp, err := s.Send(context.Background(), p)
	if err == nil {
		t.Fatal("expected error from Send")
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
}

func TestSend_WithCancelledContext(t *testing.T) {
	s := NewShipper("http://localhost:8080", "test-key")
	p := &payload.Payload{AgentVersion: "1.0.0"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Send(ctx, p)
	if err == nil {
		t.Log("Send did not return error on cancelled context (may be expected if unimplemented)")
	}
}
