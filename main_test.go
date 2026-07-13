package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/buffer"
	"github.com/ProjectEllysia/Ellysia-Hygeia/collector"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	"github.com/ProjectEllysia/Ellysia-Hygeia/shipper"
)

func TestCollectPayload_ReturnsHostInfo(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cs := []collector.Collector{}
	p := collectPayload(context.Background(), log, cs)

	if p.AgentVersion != AgentVersion {
		t.Errorf("AgentVersion = %q, want %q", p.AgentVersion, AgentVersion)
	}
	if p.Host.Hostname == "" {
		t.Errorf("expected non-empty hostname")
	}
	if p.CollectedAt.IsZero() {
		t.Errorf("expected non-zero CollectedAt")
	}
}

func TestCollectPayload_WithCollectors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	r := collector.NewRegistry()
	cs := r.Build([]string{"cpu", "memory"})

	p := collectPayload(context.Background(), log, cs)
	if p == nil {
		t.Fatal("expected non-nil payload")
	}
	if p.Metrics.CPU != nil {
		t.Log("CPU metrics populated (may be nil if not implemented)")
	}
}

func TestCollectPayload_WithAllCollectors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	r := collector.NewRegistry()
	cs := r.Build(nil)

	p := collectPayload(context.Background(), log, cs)
	if len(p.Host.Hostname) == 0 {
		t.Error("expected non-empty hostname")
	}
}

func TestDrainBuffer_EmptyBuffer(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	buf := buffer.NewRingBuffer("", 10)
	shp := shipper.NewShipper("http://localhost", "key")

	drainBuffer(context.Background(), log, buf, shp)
}

func TestDrainBuffer_WithItems(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	buf := buffer.NewRingBuffer("", 10)
	shp := shipper.NewShipper("http://localhost", "key")

	if err := buf.Push(&payload.Payload{AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	drainBuffer(context.Background(), log, buf, shp)
}

func TestRunOnce_WithCollectors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	r := collector.NewRegistry()
	cs := r.Build([]string{"cpu"})
	buf := buffer.NewRingBuffer("", 10)
	shp := shipper.NewShipper("http://localhost:8080", "test-key")

	runOnce(context.Background(), log, cs, buf, shp)
}

func TestRunOnceWithCanceledContext(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cs := []collector.Collector{}
	buf := buffer.NewRingBuffer("", 10)
	shp := shipper.NewShipper("http://localhost:8080", "test-key")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runOnce(ctx, log, cs, buf, shp)
}

func TestCollectPayloadTimeout(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cs := []collector.Collector{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	time.Sleep(time.Millisecond)

	p := collectPayload(ctx, log, cs)
	if p == nil {
		t.Fatal("expected payload even with expired context")
	}
}
