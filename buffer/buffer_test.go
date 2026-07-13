package buffer

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestNewRingBuffer(t *testing.T) {
	b := NewRingBuffer("test.jsonl", 100)
	if b == nil {
		t.Fatal("expected non-nil buffer")
	}
	if b.path != "test.jsonl" {
		t.Errorf("path = %q, want %q", b.path, "test.jsonl")
	}
	if b.maxItems != 100 {
		t.Errorf("maxItems = %d, want %d", b.maxItems, 100)
	}
}

func TestRingBuffer_Len_Empty(t *testing.T) {
	b := NewRingBuffer("test.jsonl", 10)
	if got := b.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}

func TestRingBuffer_Pop_Empty(t *testing.T) {
	b := NewRingBuffer("test.jsonl", 10)
	p, err := b.Pop()
	if err != io.EOF {
		t.Errorf("Pop() = (%v, %v), want (nil, io.EOF)", p, err)
	}
	if p != nil {
		t.Errorf("Pop() = (%v, %v), want (nil, io.EOF)", p, err)
	}
}

func TestRingBuffer_PushPop(t *testing.T) {
	t.Skip("TODO: Push/Pop/Len sin implementar")
	dir := t.TempDir()
	path := filepath.Join(dir, "buffer.jsonl")
	b := NewRingBuffer(path, 10)

	p := &payload.Payload{
		AgentVersion: "1.0.0",
		Host:         payload.HostInfo{Hostname: "test-host"},
	}

	if err := b.Push(p); err != nil {
		t.Fatalf("Push() = %v", err)
	}
	if got := b.Len(); got != 1 {
		t.Errorf("Len() after push = %d, want 1", got)
	}
}

func TestRingBuffer_PushOverLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "buffer.jsonl")
	b := NewRingBuffer(path, 2)

	for i := 0; i < 5; i++ {
		p := &payload.Payload{AgentVersion: "test"}
		if err := b.Push(p); err != nil {
			t.Fatalf("Push #%d: %v", i, err)
		}
	}
}

func TestRingBuffer_PopAfterPush(t *testing.T) {
	t.Skip("TODO: Push/Pop sin implementar")
	dir := t.TempDir()
	path := filepath.Join(dir, "buffer.jsonl")
	b := NewRingBuffer(path, 10)

	push := &payload.Payload{
		AgentVersion: "1.0.0",
		Host:         payload.HostInfo{Hostname: "test-host"},
	}
	if err := b.Push(push); err != nil {
		t.Fatalf("Push() = %v", err)
	}

	got, err := b.Pop()
	if err != nil {
		t.Fatalf("Pop() = %v", err)
	}
	if got.Host.Hostname != push.Host.Hostname {
		t.Errorf("Pop().Hostname = %q, want %q", got.Host.Hostname, push.Host.Hostname)
	}
}

func TestRingBuffer_PersistsAcrossInstances(t *testing.T) {
	t.Skip("TODO: persistencia en disco sin implementar")
	dir := t.TempDir()
	path := filepath.Join(dir, "buffer.jsonl")
	b1 := NewRingBuffer(path, 10)

	if err := b1.Push(&payload.Payload{AgentVersion: "v1"}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	b2 := NewRingBuffer(path, 10)
	if got := b2.Len(); got != 1 {
		t.Errorf("Len() after re-open = %d, want 1", got)
	}

	got, err := b2.Pop()
	if err != nil {
		t.Fatalf("Pop() = %v", err)
	}
	if got.AgentVersion != "v1" {
		t.Errorf("AgentVersion = %q, want %q", got.AgentVersion, "v1")
	}
}

func TestCleanupTempDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "buffer.jsonl")

	NewRingBuffer(path, 5)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("temp dir should exist")
	}
}
