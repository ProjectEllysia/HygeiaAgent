// Command hygeia-agent es el servicio headless: recolecta métricas y las
// empuja al backend de Ellysia. Corre como servicio del SO (§6) —
// systemd/servicio de Windows/launchd, abstraídos por kardianos/service — y
// expone el canal de control local para el companion de bandeja (§11.4).
//
// Uso:
//
//	hygeia-agent              ejecuta en primer plano (desarrollo)
//	hygeia-agent install      registra el servicio en el SO
//	hygeia-agent uninstall    lo elimina
//	hygeia-agent start|stop|restart
//	hygeia-agent status       estado del servicio según el SO
//	hygeia-agent debug        diagnóstico del proceso en marcha
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/kardianos/service"

	"github.com/ProjectEllysia/Ellysia-Hygeia/agent"
	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/logring"
)

// program implementa service.Interface: el SO llama a Start (que NO debe
// bloquear) y a Stop al parar el servicio.
type program struct {
	log       *slog.Logger
	agent     *agent.Agent
	recentLog control.RecentLogFunc
	cancel    context.CancelFunc
	// wg espera a AMBAS goroutines lanzadas en Start (bucle del agente y
	// canal de control) antes de que Stop() devuelva. Antes solo se esperaba
	// al bucle del agente — el servicio podía reportarse "parado" al SO con
	// el pipe/socket del canal de control todavía cerrándose (plan §12.2,
	// Tier 2).
	wg sync.WaitGroup
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// Canal de control: si no se puede abrir, el agente sigue funcionando
	// perfectamente — el tray es opcional (§11.6) y no debe poder tumbar la
	// monitorización.
	srv := control.NewServer(p.log, p.agent.Status, p.agent.Enroll, p.agent.Reset, p.recentLog)

	p.wg.Add(2)
	go func() {
		defer p.wg.Done()
		if err := srv.Serve(ctx); err != nil {
			p.log.Error("canal de control no disponible (el agente sigue)", "err", err)
		}
	}()
	go func() {
		defer p.wg.Done()
		p.agent.Run(ctx)
	}()
	return nil
}

func (p *program) Stop(service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return nil
}

func main() {
	log, logRing := newLogger()

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		log.Error("error cargando configuración", "err", err)
		os.Exit(1)
	}

	svcConfig := &service.Config{
		Name:        "hygeia-agent",
		DisplayName: "Ellysia Hygeia Agent",
		Description: "Recolecta métricas de salud del activo y las envía al backend de Ellysia.",
		Option: service.KeyValue{
			// Que el SO lo reinicie si muere: un agente caído es un activo
			// mudo, que es indistinguible de un activo apagado.
			"Restart":         "always",
			"OnFailure":       "restart",
			"RestartSec":      10,
			"StartLimitBurst": 0,
		},
	}

	prg := &program{log: log, agent: agent.New(log, cfg), recentLog: logRing.Lines}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Error("no se pudo construir el servicio", "err", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		if err := runCommand(svc, os.Args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "hygeia-agent: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Sin argumentos: service.Run detecta solo si lo ha arrancado el SO (y
	// entonces habla con el gestor de servicios) o si es una ejecución
	// interactiva en terminal, donde bloquea hasta Ctrl+C.
	if err := svc.Run(); err != nil {
		log.Error("el servicio terminó con error", "err", err)
		os.Exit(1)
	}
}

// logRingCapacity es cuántas líneas recientes retiene el logger para
// GET /debug (plan §12.2, Tier 3) — un puñado de líneas basta para ver qué
// estaba pasando justo antes de un problema, sin guardar un historial largo
// en memoria.
const logRingCapacity = 50

// newLogger elige destino según cómo se arrancó. En terminal, stderr. Como
// servicio no hay terminal a la que mirar, así que va a un fichero en el
// directorio de estado (§5: auto-observación — un agente mudo hay que poder
// diagnosticarlo). Si el fichero no se puede abrir, stderr como último
// recurso: quedarse sin logs no justifica no arrancar.
//
// En paralelo (io.MultiWriter), cada línea también se guarda en un
// logring.Buffer en memoria, que el canal de control expone por GET /debug
// — diagnóstico de campo sin depender de encontrar el fichero de log.
func newLogger() (*slog.Logger, *logring.Buffer) {
	ring := logring.New(logRingCapacity)

	if service.Interactive() {
		return slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, ring), nil)), ring
	}
	logPath := filepath.Join(config.DataDir(), "hygeia-agent.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			return slog.New(slog.NewTextHandler(io.MultiWriter(f, ring), nil)), ring
		}
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, ring), nil)), ring
}
