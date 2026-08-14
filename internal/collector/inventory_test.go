package collector

import (
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// HKEY_USERS contiene bastante más que usuarios. El filtro es lo que decide
// qué ramas se recorren, así que conviene fijar los dos lados.
func TestIsUserSID(t *testing.T) {
	users := []string{
		"S-1-5-21-1234567890-1234567890-1234567890-1001", // cuenta local
		"S-1-5-21-9876543210-1111111111-2222222222-500",  // administrador
	}
	for _, sid := range users {
		if !isUserSID(sid) {
			t.Errorf("isUserSID(%q) = false, es una cuenta de usuario real", sid)
		}
	}

	notUsers := map[string]string{
		"S-1-5-18": "LocalSystem, la cuenta del propio servicio",
		"S-1-5-19": "LocalService",
		"S-1-5-20": "NetworkService",
		".DEFAULT": "el perfil plantilla, no el de nadie",
		"S-1-5-21-1234567890-1234567890-1234567890-1001_Classes": "la vista " +
			"de HKEY_CLASSES_ROOT de ese usuario: asociaciones de ficheros, " +
			"no programas instalados",
	}
	for sid, why := range notUsers {
		if isUserSID(sid) {
			t.Errorf("isUserSID(%q) = true, pero es %s", sid, why)
		}
	}
}

// Un programa instalado para todos los usuarios aparece en la rama de la
// máquina y en la de cada perfil cargado.
func TestDedupeSoftwareKeepsOneEntryPerInstallation(t *testing.T) {
	list := []payload.Software{
		{Name: "Google Chrome", Version: "120.0", Architecture: "x64"},
		{Name: "Google Chrome", Version: "120.0", Architecture: "x64"}, // otro perfil
		{Name: "Google Chrome", Version: "120.0", Architecture: "x64"}, // y otro
		{Name: "7-Zip", Version: "24.09", Architecture: "x64"},
	}

	got := dedupeSoftware(list)

	if len(got) != 2 {
		t.Fatalf("se obtuvieron %d entradas, se esperaban 2: %+v", len(got), got)
	}
	if got[0].Name != "Google Chrome" || got[1].Name != "7-Zip" {
		t.Errorf("se conservó el orden equivocado: %+v", got)
	}
}

// Dos versiones del mismo programa, o la de 32 y la de 64 bits, son dos
// instalaciones reales: fundirlas escondería la que esté sin actualizar.
func TestDedupeSoftwareDistinguishesVersionsAndArchitectures(t *testing.T) {
	list := []payload.Software{
		{Name: "Python", Version: "3.11.9", Architecture: "x64"},
		{Name: "Python", Version: "3.12.4", Architecture: "x64"},
		{Name: "Python", Version: "3.12.4", Architecture: "x86"},
	}

	if got := dedupeSoftware(list); len(got) != 3 {
		t.Errorf("se obtuvieron %d entradas, se esperaban 3: %+v", len(got), got)
	}
}

// El separador de la clave no puede confundir dos entradas distintas cuyos
// campos concatenados coincidan.
func TestDedupeSoftwareDoesNotConflateAdjacentFields(t *testing.T) {
	list := []payload.Software{
		{Name: "AB", Version: "C"},
		{Name: "A", Version: "BC"},
	}

	if got := dedupeSoftware(list); len(got) != 2 {
		t.Errorf("se obtuvieron %d entradas, se esperaban 2: %+v", len(got), got)
	}
}

func TestDedupeSoftwareOnAnEmptyList(t *testing.T) {
	got := dedupeSoftware(nil)

	if got == nil {
		t.Error("dedupeSoftware(nil) = nil: un slice nulo se serializa como null")
	}
	if len(got) != 0 {
		t.Errorf("dedupeSoftware(nil) = %+v, se esperaba vacío", got)
	}
}
