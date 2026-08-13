package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kardianos/service"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/version"
)

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

	case "debug":
		return runDebug()

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

// runDebug se conecta al canal de control de un servicio YA EN MARCHA y
// vuelca su GET /debug (plan §12.2, Tier 3) — diagnóstico de campo sin
// depender de encontrar el fichero de log.
func runDebug() error {
	client := control.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := client.Debug(ctx)
	if err != nil {
		return fmt.Errorf("debug: %w (¿está el servicio en marcha?)", err)
	}

	fmt.Printf("goroutines: %d\n", info.Goroutines)
	fmt.Printf("memoria:    alloc=%d KB  sys=%d KB  numGC=%d\n",
		info.AllocBytes/1024, info.SysBytes/1024, info.NumGC)
	if len(info.RecentLog) == 0 {
		fmt.Println("log reciente: (vacío)")
		return nil
	}
	fmt.Println("log reciente:")
	for _, line := range info.RecentLog {
		fmt.Println("  " + line)
	}
	return nil
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
  debug             diagnóstico del proceso en marcha (goroutines, memoria, log reciente)
  version
`
