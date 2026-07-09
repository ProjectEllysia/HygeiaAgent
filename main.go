package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/buffer"
	"github.com/ProjectEllysia/Ellysia-Hygeia/collector"
	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	"github.com/ProjectEllysia/Ellysia-Hygeia/shipper"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load("config.toml")
	if err != nil {
		log.Error("error cargando configuración", "err", err)
		os.Exit(1)
	}

	registry := collector.NewRegistry()
	collectors := registry.Build(cfg.Collectors)
	buf := buffer.NewRingBuffer(cfg.BufferPath, 1000)
	shp := shipper.NewShipper(cfg.ServerURL, cfg.AgentKey)

	// signal.NotifyContext cancela ctx al recibir Ctrl+C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	interval := time.Duration(cfg.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Info("hygeia iniciado",
		"version", AgentVersion,
		"interval", interval,
		"serverUrl", cfg.ServerURL,
		"collectors", len(collectors),
	)

	runOnce(ctx, log, collectors, buf, shp)
	for {
		select {
		case <-ctx.Done():
			log.Info("cerrando agente")
			return
		case <-ticker.C:
			runOnce(ctx, log, collectors, buf, shp)
		}
	}
}

// runOnce ejecuta un ciclo completo: recolectar -> enviar -> (drenar buffer).
func runOnce(
	ctx context.Context,
	log *slog.Logger,
	cs []collector.Collector,
	buf *buffer.RingBuffer,
	shp *shipper.Shipper,
) {
	p := collectPayload(ctx, log, cs)

	resp, err := shp.Send(ctx, p)
	if err != nil {
		log.Warn("envío fallido, guardando en buffer", "err", err)
		if perr := buf.Push(p); perr != nil {
			log.Error("no se pudo guardar en buffer", "err", perr)
		}
		return
	}
	log.Info("heartbeat enviado", "nextIntervalSec", resp.NextIntervalSec)
	// TODO: si resp.NextIntervalSec difiere del intervalo actual, reconstruir
	// el ticker para auto-ajustar el agente sin re-desplegar (README §9).
	drainBuffer(ctx, log, buf, shp)
}

// collectPayload lanza los colectores en paralelo (goroutines) con timeout
// individual y ensambla el payload. Cada colector escribe un campo distinto
// de p.Metrics, por lo que no hace falta mutex.
func collectPayload(ctx context.Context, log *slog.Logger, cs []collector.Collector) *payload.Payload {
	p := &payload.Payload{
		AgentVersion: AgentVersion,
		CollectedAt:  time.Now().UTC(),
		Host:         collector.Host(),
	}

	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := c.Collect(cctx, &p.Metrics); err != nil {
				log.Warn("colector falló", "name", c.Name(), "err", err)
			}
		}(c)
	}
	wg.Wait()
	return p
}

// drainBuffer envía los payloads aplazados mientras el backend responda.
func drainBuffer(ctx context.Context, log *slog.Logger, buf *buffer.RingBuffer, shp *shipper.Shipper) {
	for {
		p, err := buf.Pop()
		if err != nil {
			return
		}
		if _, err := shp.Send(ctx, p); err != nil {
			log.Warn("drenado interrumpido, reintentará más tarde", "err", err)
			return
		}
	}
}
