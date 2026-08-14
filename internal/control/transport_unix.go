//go:build !windows

package control

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// socketPath es el socket Unix del canal de control. Vive en el directorio
// de estado del servicio y nunca se expone en una interfaz de red (§11.7).
func socketPath() string {
	if v := os.Getenv("HYGEIA_CONTROL_SOCKET"); v != "" {
		return v
	}
	if _, err := os.Stat("/run"); err == nil {
		return "/run/hygeia-agent.sock"
	}
	return "/tmp/hygeia-agent.sock"
}

// Address devuelve la dirección legible del canal, para logs y diagnóstico.
func Address() string { return socketPath() }

// Listen abre el socket Unix del lado del servicio con permisos 0660: el
// dueño (cuenta del servicio) y el grupo pueden hablar; el resto de usuarios
// locales, no. La autorización es del transporte, no de confiar en que
// quien conecta es el tray (§11.7).
//
// Para que el tray (que corre como el usuario de sesión) pueda conectar,
// pon al usuario en un grupo dedicado y exporta su GID en
// HYGEIA_CONTROL_GID.
// maxSocketPath es lo que cabe en el sun_path de una dirección de socket
// Unix. El campo tiene tamaño fijo: 104 bytes en macOS y BSD, 108 en Linux.
// Se usa el menor de los dos, que es el que de verdad acota.
const maxSocketPath = 104

func Listen() (net.Listener, error) {
	path := socketPath()
	// Comprobado antes del bind porque el error del sistema no dice nada:
	// pasarse de sun_path se reporta como un "invalid argument" pelado, sin
	// mencionar la longitud ni la ruta. Tres iteraciones de CI se fueron en
	// averiguar exactamente eso.
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf(
			"control: la ruta del socket ocupa %d bytes y el máximo del sistema es %d: %q",
			len(path), maxSocketPath, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("control: creando directorio del socket: %w", err)
	}
	// Un socket huérfano de un arranque anterior impediría el bind.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("control: limpiando socket previo: %w", err)
	}

	// umask podría recortar los permisos del socket recién creado, así que
	// se fija explícitamente con Chmod justo después del bind.
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("control: abriendo socket %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("control: fijando permisos del socket: %w", err)
	}
	if v := os.Getenv("HYGEIA_CONTROL_GID"); v != "" {
		gid, err := strconv.Atoi(v)
		if err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("control: HYGEIA_CONTROL_GID inválido: %w", err)
		}
		if err := os.Chown(path, -1, gid); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("control: asignando grupo al socket: %w", err)
		}
	}
	return ln, nil
}

// dial conecta con el socket desde el tray.
func dial(ctx context.Context, _, _ string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socketPath())
}
