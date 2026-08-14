package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kardianos/service"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/version"
)

// controlTimeout acota las órdenes que hablan con el servicio en marcha.
// Generoso a propósito: enroll persiste la configuración en disco y rehace el
// shipper antes de responder.
const controlTimeout = 10 * time.Second

// runCommand despacha los subcomandos.
//
// Recibe todos los argumentos, no solo el primero, porque `enroll` lleva la
// clave detrás.
//
// Y recibe un constructor del servicio en vez del servicio ya montado, porque
// montarlo exige leer config.toml y la mitad de los subcomandos no lo
// necesita: enroll, reset, info y debug hablan con el servicio EN MARCHA, que
// tiene su propia configuración cargada, y version y help no hablan con nadie.
// Cargar la configuración por adelantado hacía imposible dar de alta un agente
// recién instalado, que es justo para lo que existe `enroll`.
func runCommand(buildService func() (service.Service, error), args []string) error {
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "install", "uninstall", "start", "stop", "restart":
		svc, err := buildService()
		if err != nil {
			return fmt.Errorf("%s: %w", cmd, err)
		}
		if err := service.Control(svc, cmd); err != nil {
			return fmt.Errorf("%s: %w (¿lo estás ejecutando como administrador/root?)", cmd, err)
		}
		fmt.Printf("hygeia-agent: %s completado\n", cmd)
		return nil

	case "status":
		svc, err := buildService()
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		st, err := svc.Status()
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		fmt.Printf("hygeia-agent: %s\n", statusName(st))
		return nil

	case "enroll":
		return runEnroll(rest)

	case "reset":
		return runReset(rest)

	case "info":
		return runInfo()

	case "doctor":
		return runDoctor()

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

// runEnroll entrega la clave de agente al servicio EN MARCHA por el canal de
// control, que la valida y la persiste.
//
// Es el equivalente por línea de órdenes de lo que hace el icono de bandeja.
// Hasta ahora, dar de alta un servidor sin escritorio obligaba a editar
// config.toml a mano por SSH, con privilegios, y reiniciar el servicio — en la
// única clase de máquina donde no hay bandeja. Con esto, un cloud-init o un
// playbook de Ansible puede instalar y dar de alta un agente sin que
// intervenga nadie.
func runEnroll(args []string) error {
	// Si la clave no viene como argumento se lee de la entrada estándar; y si
	// esa entrada es un terminal, hay que decir que se está esperando algo o
	// el mandato parece colgado.
	if len(args) == 0 && isTerminal(os.Stdin) {
		fmt.Fprint(os.Stderr, "Clave de agente: ")
	}

	key, err := agentKey(args, os.Stdin)
	if err != nil {
		return err
	}

	client := control.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()

	if err := client.Enroll(ctx, key); err != nil {
		return fmt.Errorf("enroll: %w (¿está el servicio en marcha?)", err)
	}
	fmt.Println("hygeia-agent: agente dado de alta")
	return nil
}

// agentKey obtiene la clave del argumento o de la entrada estándar.
//
// Se admiten las dos formas porque sirven para cosas distintas, pero NO son
// equivalentes en seguridad: una clave pasada como argumento queda visible
// para cualquier usuario de la máquina con `ps`, y se queda en el historial
// del intérprete. De ahí el aviso, y de ahí que la entrada estándar sea la
// forma recomendada:
//
//	echo "$CLAVE" | hygeia-agent enroll
func agentKey(args []string, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		if key := strings.TrimSpace(args[0]); key != "" {
			fmt.Fprintln(os.Stderr, "hygeia-agent: aviso — una clave pasada como argumento "+
				"queda visible en `ps` y en el historial del intérprete;")
			fmt.Fprintln(os.Stderr, "              para automatizar, prefiere "+
				"`echo \"$CLAVE\" | hygeia-agent enroll`")
			return key, nil
		}
	}

	line, err := bufio.NewReader(stdin).ReadString('\n')
	// EOF con contenido delante es lo normal al canalizar sin salto final.
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("enroll: leyendo la clave de la entrada estándar: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New("enroll: no se indicó ninguna clave\n\n" +
			"  hygeia-agent enroll <clave>\n" +
			"  echo \"$CLAVE\" | hygeia-agent enroll")
	}
	return key, nil
}

// runReset borra la clave local: el agente deja de reportar hasta que se le dé
// de alta otra vez.
//
// Nunca borra nada sin que alguien lo haya dicho, y no por prudencia
// genérica: `reset` y `restart` se parecen lo bastante como para teclear uno
// por otro, y confundirse deja el activo mudo sin avisar de nada.
//
// Desde un terminal, pregunta. Sin terminal —un script, un playbook, un
// `reset` con la entrada redirigida— NO asume que sí: exige --yes y falla si
// no está. La alternativa habitual, "sin terminal se da por confirmado", deja
// abierto justo el camino silencioso que se quiere cerrar, y a un script
// legítimo escribir --yes una vez no le cuesta nada.
func runReset(args []string) error {
	return resetWith(args, isTerminal(os.Stdin), os.Stdin)
}

// resetWith recibe la interactividad en vez de deducirla, para que se pueda
// probar en los dos sentidos. No es un adorno: bajo `go test` en Windows la
// entrada estándar resulta ser un terminal, así que una prueba que confiara en
// isTerminal comprobaría una cosa en Windows y la contraria en el runner de
// Linux, sin decir nada.
func resetWith(args []string, interactive bool, stdin io.Reader) error {
	if !slices.Contains(args, "--yes") && !slices.Contains(args, "-y") {
		if !interactive {
			return errors.New("reset: esto borra la clave y el agente dejará de reportar;\n" +
				"no hay terminal donde confirmarlo, así que hay que decirlo explícitamente:\n\n" +
				"  hygeia-agent reset --yes")
		}
		fmt.Fprint(os.Stderr, "Esto borrará la clave y el agente dejará de reportar. "+
			"¿Seguro? [s/N]: ")
		if !confirmed(stdin) {
			fmt.Println("hygeia-agent: cancelado")
			return nil
		}
	}

	client := control.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()

	if err := client.Reset(ctx); err != nil {
		return fmt.Errorf("reset: %w (¿está el servicio en marcha?)", err)
	}
	fmt.Println("hygeia-agent: clave borrada, el agente queda sin configurar")
	return nil
}

func confirmed(stdin io.Reader) bool {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "si", "sí", "y", "yes":
		return true
	default:
		return false
	}
}

// runInfo pregunta al PROPIO agente cómo se encuentra.
//
// No es lo mismo que `status`, que le pregunta al gestor de servicios del
// sistema si el proceso está vivo. Un agente puede estar perfectamente en
// ejecución y llevar horas sin poder entregar un heartbeat.
func runInfo() error {
	client := control.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()

	st, err := client.Status(ctx)
	if err != nil {
		return fmt.Errorf("info: %w (¿está el servicio en marcha?)", err)
	}

	fmt.Printf("estado:       %s\n", st.State)
	fmt.Printf("versión:      %s\n", st.AgentVersion)
	if st.Hostname != "" {
		fmt.Printf("equipo:       %s\n", st.Hostname)
	}
	if st.ServerURL != "" {
		fmt.Printf("servidor:     %s\n", st.ServerURL)
	}
	fmt.Printf("buffer:       %d payloads pendientes de entregar\n", st.BufferSize)
	if st.LastPushAt.IsZero() {
		fmt.Println("último envío: nunca")
	} else {
		fmt.Printf("último envío: %s (hace %s)\n",
			st.LastPushAt.Format(time.RFC3339),
			time.Since(st.LastPushAt).Truncate(time.Second))
	}
	if st.LastError != "" {
		fmt.Printf("último error: %s\n", st.LastError)
	}
	return nil
}

// isTerminal distingue una ejecución interactiva de una canalizada.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
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

Servicio del sistema:
  install           registra el servicio en el SO
  uninstall         elimina el servicio
  start | stop | restart
  status            pregunta al gestor de servicios si el proceso está vivo

Alta y diagnóstico (hablan con el agente en marcha):
  enroll [clave]    da de alta el agente; sin argumento, lee la clave de la
                    entrada estándar, que es la forma recomendada porque un
                    argumento queda visible en ` + "`ps`" + `
  reset [--yes]     borra la clave; el agente deja de reportar
  info              estado del propio agente: conexión, buffer, último envío
  doctor            comprueba por qué no llega: config, DNS, TCP, TLS, clave,
                    reloj y buffer, con un consejo por cada fallo
  debug             interioridades del proceso: goroutines, memoria, log reciente

  version

Ejemplos:
  echo "$CLAVE" | hygeia-agent enroll
  hygeia-agent info
  hygeia-agent doctor
`
