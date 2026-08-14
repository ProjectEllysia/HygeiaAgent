package control

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolate apunta el canal de control a un transporte propio del test.
//
// Sin esto, los tests usarían el pipe/socket por defecto y hablarían con el
// hygeia-agent que esté instalado y corriendo en la máquina: los asserts
// pasarían o fallarían según el estado de un servicio real, no del servidor
// bajo prueba.
func isolate(t *testing.T) {
	t.Helper()
	unique := fmt.Sprintf("hygeia-test-%d-%d", os.Getpid(), testCounter())
	if runtime.GOOS == "windows" {
		t.Setenv("HYGEIA_CONTROL_PIPE", `\\.\pipe\`+unique)
		return
	}
	t.Setenv("HYGEIA_CONTROL_SOCKET", shortSocketPath(t, unique))
}

// shortSocketPath devuelve una ruta de socket que quepa en sun_path.
//
// NO se usa t.TempDir(), que es lo natural y es lo que había: la dirección de
// un socket Unix viaja en un sun_path de tamaño fijo —104 bytes en macOS, 108
// en Linux— y en macOS os.TempDir() es
// "/var/folders/xx/<32 caracteres>/T/", 49 caracteres. Sumándole el nombre
// del test y el sufijo aleatorio que t.TempDir() añade, la ruta se iba a 120
// bytes y el bind fallaba con "invalid argument". En Linux la misma línea
// funcionaba porque allí os.TempDir() es "/tmp/", 45 caracteres menos.
//
// El caso no se da en producción: allí el socket es /run/hygeia-agent.sock o
// /tmp/hygeia-agent.sock, 22 bytes. Era el test el que no cabía.
func shortSocketPath(t *testing.T, unique string) string {
	t.Helper()
	// /tmp directamente, no os.TempDir(): es corto en los dos sistemas y es
	// además donde socketPath() cae por defecto cuando no hay /run.
	path := filepath.Join("/tmp", unique+".sock")
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

var counter int

func testCounter() int {
	counter++
	return counter
}
