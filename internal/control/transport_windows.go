package control

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
)

// defaultPipeName es el named pipe del canal de control. Un named pipe nunca
// sale de la máquina (§11.7): no hay interfaz de red implicada.
const defaultPipeName = `\\.\pipe\hygeia-agent`

// pipeName permite override por entorno, igual que HYGEIA_CONTROL_SOCKET en
// Unix. El nombre del pipe es global a la máquina: sin override, una segunda
// instancia (o un test) hablaría con el agente ya instalado en vez de con el
// suyo, que es exactamente el fallo que destapó la prueba de integración.
func pipeName() string {
	if v := os.Getenv("HYGEIA_CONTROL_PIPE"); v != "" {
		return v
	}
	return defaultPipeName
}

// pipeSDDL restringe quién puede abrir el pipe. La autorización viene de
// aquí, no de confiar en que quien conecta es de fiar (§11.7):
//
//	D:P            descriptor protegido (no hereda ACEs del padre)
//	(A;;GA;;;SY)   LocalSystem (la cuenta del servicio): control total
//	(A;;GA;;;BA)   Administradores: control total
//	(A;;GRGW;;;IU) Usuarios interactivos (la sesión de escritorio donde vive
//	               el tray): solo leer y escribir en el pipe
//
// Deliberadamente NO se concede a "Everyone" ni a cuentas de servicio de
// red: un servicio remoto o una tarea sin sesión no tiene nada que hacer
// aquí.
const pipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"

// Address devuelve la dirección legible del canal, para logs y diagnóstico.
func Address() string { return pipeName() }

// Listen abre el named pipe del lado del servicio.
func Listen() (net.Listener, error) {
	return winio.ListenPipe(pipeName(), &winio.PipeConfig{
		SecurityDescriptor: pipeSDDL,
		MessageMode:        false,
	})
}

// dial conecta con el pipe desde el tray.
func dial(ctx context.Context, _, _ string) (net.Conn, error) {
	timeout := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	return winio.DialPipe(pipeName(), &timeout)
}
