package main

import (
	"fmt"

	"github.com/kardianos/service"

	"github.com/ProjectEllysia/Ellysia-Hygeia/version"
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
