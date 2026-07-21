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
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kardianos/service"

	"github.com/ProjectEllysia/Ellysia-Hygeia/agent"
	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/version"
)

// program implementa service.Interface: el SO llama a Start (que NO debe
// bloquear) y a Stop al parar el servicio.
type program struct {
	log    *slog.Logger
	agent  *agent.Agent
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})

	// Canal de control: si no se puede abrir, el agente sigue funcionando
	// perfectamente — el tray es opcional (§11.6) y no debe poder tumbar la
	// monitorización.
	srv := control.NewServer(p.log, p.agent.Status, p.agent.Enroll)
	go func() {
		if err := srv.Serve(ctx); err != nil {
			p.log.Error("canal de control no disponible (el agente sigue)", "err", err)
		}
	}()

	go func() {
		defer close(p.done)
		p.agent.Run(ctx)
	}()
	return nil
}

func (p *program) Stop(service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	return nil
}

func main() {
	log := newLogger()

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

	prg := &program{log: log, agent: agent.New(log, cfg)}
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

// runCommand despacha los subcomandos de gestión del servicio.
func runCommand(svc service.Service, cmd string) error {
	switch cmd {
	case "install", "uninstall", "start", "stop", "restart":
		if err := service.Control(svc, cmd); err != nil {
			return fmt.Errorf("%s: %w (¿lo estás ejecutando como administrador/root?)", cmd, err)
		}
		fmt.Printf("hygeia-agent: %s completado\n", cmd)
		return nil

	case "status":
		st, err := svc.Status()
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		fmt.Printf("hygeia-agent: %s\n", statusName(st))
		return nil

	case "version", "-v", "--version":
		fmt.Printf("hygeia-agent %s\n", version.Version)
		return nil

	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil

	default:
		return fmt.Errorf("subcomando desconocido %q\n\n%s", cmd, usage)
	}
}

func statusName(st service.Status) string {
	switch st {
	case service.StatusRunning:
		return "en ejecución"
	case service.StatusStopped:
		return "parado"
	default:
		return "desconocido (¿no está instalado?)"
	}
}

const usage = `Uso: hygeia-agent [subcomando]

  (sin argumentos)  ejecuta el agente en primer plano
  install           registra el servicio en el SO
  uninstall         elimina el servicio
  start | stop | restart
  status            consulta el estado al gestor de servicios
  version
`

// newLogger elige destino según cómo se arrancó. En terminal, stderr. Como
// servicio no hay terminal a la que mirar, así que va a un fichero en el
// directorio de estado (§5: auto-observación — un agente mudo hay que poder
// diagnosticarlo). Si el fichero no se puede abrir, stderr como último
// recurso: quedarse sin logs no justifica no arrancar.
func newLogger() *slog.Logger {
	if service.Interactive() {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	logPath := filepath.Join(config.DataDir(), "hygeia-agent.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			return slog.New(slog.NewTextHandler(f, nil))
		}
	}
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}
