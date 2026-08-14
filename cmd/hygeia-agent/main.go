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
//	hygeia-agent enroll       da de alta el agente sin necesidad del tray
//	hygeia-agent reset        borra la clave local
//	hygeia-agent info         estado del propio agente: conexión, buffer, envíos
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

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/agent"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
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
	// El nivel es una LevelVar y no un valor fijo porque el logger tiene que
	// existir ANTES de cargar la config —si la carga falla, ese error hay que
	// poder registrarlo— pero el nivel lo decide la propia config. Se arranca
	// en Info y se ajusta en cuanto se sabe.
	var level slog.LevelVar
	log, logRing := newLogger(&level)

	// La configuración se carga PEREZOSAMENTE, solo cuando el subcomando la
	// necesita de verdad.
	//
	// Antes se cargaba aquí, antes de mirar siquiera qué se había pedido, y
	// eso rompía justo el caso para el que existe `enroll`: en un agente
	// recién instalado puede no haber config.toml todavía —`enroll` es lo que
	// lo crea—, y leerlo exige privilegios que quien da de alta no tiene por
	// qué tener. `hygeia-agent enroll` moría con "error cargando
	// configuración" en la única situación en la que hace falta. Y `help` y
	// `version` morían igual, sin necesitar nada.
	//
	// enroll, reset, info, debug, version y help no tocan la configuración:
	// hablan con el servicio en marcha, que tiene la suya.
	buildService := func() (service.Service, error) {
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			return nil, fmt.Errorf("cargando configuración: %w", err)
		}
		level.Set(cfg.SlogLevel())

		prg := &program{log: log, agent: agent.New(log, cfg), recentLog: logRing.Lines}
		return service.New(prg, svcConfig())
	}

	if len(os.Args) > 1 {
		if err := runCommand(buildService, os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "hygeia-agent: %v\n", err)
			os.Exit(1)
		}
		return
	}

	svc, err := buildService()
	if err != nil {
		log.Error("no se pudo construir el servicio", "err", err)
		os.Exit(1)
	}

	// Sin argumentos: service.Run detecta solo si lo ha arrancado el SO (y
	// entonces habla con el gestor de servicios) o si es una ejecución
	// interactiva en terminal, donde bloquea hasta Ctrl+C.
	if err := svc.Run(); err != nil {
		log.Error("el servicio terminó con error", "err", err)
		os.Exit(1)
	}
}

func svcConfig() *service.Config {
	return &service.Config{
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
// El fichero se rota al superar maxLogBytes (ver logfile.go): antes se abría
// en modo añadir y no se rotaba nunca, lo que contradecía el principio del §5
// de que nada crece sin límite.
func newLogger(level slog.Leveler) (*slog.Logger, *logring.Buffer) {
	ring := logring.New(logRingCapacity)
	opts := &slog.HandlerOptions{Level: level}

	newWith := func(w io.Writer) *slog.Logger {
		return slog.New(slog.NewTextHandler(io.MultiWriter(w, ring), opts))
	}

	if service.Interactive() {
		return newWith(os.Stderr), ring
	}
	logPath := filepath.Join(config.DataDir(), "hygeia-agent.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if f, err := newRotatingWriter(logPath, maxLogBytes); err == nil {
			return newWith(f), ring
		}
	}
	return newWith(os.Stderr), ring
}
