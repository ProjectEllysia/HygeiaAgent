//go:build !windows

package control

import (
	"os"
	"strings"
	"testing"
)

// La dirección de un socket Unix viaja en un sun_path de tamaño fijo: 104
// bytes en macOS y BSD, 108 en Linux. Pasarse no da un error legible, da un
// "invalid argument" pelado.
//
// Esta prueba corre en TODAS las plataformas tipo Unix, no solo donde el
// límite aprieta, y comprueba contra el menor de los dos. Es el punto: la
// versión anterior de isolate() ponía el socket bajo t.TempDir(), que en
// Linux mide unos 76 bytes y en macOS 120, porque allí os.TempDir() es
// "/var/folders/xx/<32 caracteres>/T/" en vez de "/tmp/". El resultado fue
// una suite verde en Linux y ocho tests rojos en macOS con un mensaje que no
// mencionaba ni el socket ni su longitud.
func TestSocketPathFitsInSunPath(t *testing.T) {
	isolate(t)

	path := socketPath()
	if len(path) > maxSocketPath {
		t.Errorf("la ruta del socket de test ocupa %d bytes, el máximo es %d: %q",
			len(path), maxSocketPath, path)
	}
}

// El mismo límite, para la ruta que se usa en producción.
func TestDefaultSocketPathFitsInSunPath(t *testing.T) {
	t.Setenv("HYGEIA_CONTROL_SOCKET", "")

	path := socketPath()
	if len(path) > maxSocketPath {
		t.Errorf("la ruta por defecto ocupa %d bytes, el máximo es %d: %q",
			len(path), maxSocketPath, path)
	}
}

// Y si alguien configura una ruta imposible, Listen debe decirlo con
// claridad en vez de propagar el "invalid argument" del sistema.
func TestListenRejectsAnOverlongSocketPath(t *testing.T) {
	long := "/tmp/" + strings.Repeat("x", maxSocketPath) + ".sock"
	t.Setenv("HYGEIA_CONTROL_SOCKET", long)

	ln, err := Listen()
	if err == nil {
		_ = ln.Close()
		t.Fatal("Listen() con una ruta demasiado larga = nil, se esperaba error")
	}
	for _, want := range []string{"bytes", "máximo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el error no explica el problema (%q no aparece): %v", want, err)
		}
	}
}

// Cerrar el listener NO debe borrar el fichero del socket. Go lo hace por
// defecto, y en un reinicio del servicio eso es una carrera perdida: el
// proceso saliente cierra su listener después de que el entrante haya hecho
// bind sobre la misma ruta, y se lleva por delante un socket que ya es de
// otro. El resultado en campo fue un agente sano al que `doctor` daba por
// caído, porque el canal de control había dejado de existir en disco.
func TestClosingTheListenerKeepsTheSocketFile(t *testing.T) {
	isolate(t)

	ln, err := Listen()
	if err != nil {
		t.Fatalf("Listen() = %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	if _, err := os.Stat(socketPath()); err != nil {
		t.Errorf("el fichero del socket desapareció al cerrar el listener: %v", err)
	}
}

// La contrapartida del test anterior: si el cierre ya no limpia, el arranque
// siguiente tiene que poder reutilizar la ruta. Eso lo cubre el borrado del
// socket huérfano que Listen hace antes del bind.
func TestListenReusesThePathLeftByAPreviousProcess(t *testing.T) {
	isolate(t)

	first, err := Listen()
	if err != nil {
		t.Fatalf("primer Listen() = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	second, err := Listen()
	if err != nil {
		t.Fatalf("segundo Listen() sobre la ruta que dejó el anterior = %v", err)
	}
	_ = second.Close()
}
