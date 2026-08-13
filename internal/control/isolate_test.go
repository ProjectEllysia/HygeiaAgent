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
	t.Setenv("HYGEIA_CONTROL_SOCKET", filepath.Join(t.TempDir(), unique+".sock"))
}

var counter int

func testCounter() int {
	counter++
	return counter
}
