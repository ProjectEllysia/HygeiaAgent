package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// La clave de agente no puede quedar legible por el resto de usuarios
// locales (§5). En Windows el modo 0600 de os.WriteFile no hace nada, así
// que hay que comprobar la DACL real del fichero, no el modo.
func TestSaveRestrictsACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	c := &Config{
		ServerURL: "https://ellysia.example/hygeia",
		AgentKey:  "abcd1234.0123456789abcdef",
		path:      path,
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("leyendo la ACL: %v", err)
	}
	sddl := sd.String()
	t.Logf("DACL resultante: %s", sddl)

	// "P" = DACL protegida: sin ella, las ACEs permisivas del directorio
	// padre (en ProgramData, lectura para Users) se heredarían igualmente.
	if !strings.Contains(sddl, "D:P") {
		t.Errorf("la DACL no está protegida contra herencia: %s", sddl)
	}

	// Nadie más que SYSTEM, Administradores y el dueño del proceso.
	forbidden := map[string]string{
		"WD": "Everyone",
		"BU": "Users",
		"AU": "Authenticated Users",
		"IU": "Interactive Users",
	}
	for sid, name := range forbidden {
		if strings.Contains(sddl, ";"+sid+")") {
			t.Errorf("la ACL concede acceso a %s (%s): %s", name, sid, sddl)
		}
	}
}

// Un usuario que ejecuta el agente en primer plano debe conservar acceso a
// su propia config: si la ACL solo tuviera SYSTEM y Administradores, se
// bloquearía a sí mismo fuera del fichero que acaba de escribir.
func TestSaveKeepsOwnerAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	c := &Config{
		ServerURL: "https://ellysia.example/hygeia",
		AgentKey:  "abcd1234.0123456789abcdef",
		path:      path,
	}
	if err := c.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.ReadFile(path); err != nil {
		t.Errorf("el proceso no puede releer su propia config: %v", err)
	}
	// Y debe poder reescribirla (rotación de clave, cambio de intervalo).
	if err := c.Save(); err != nil {
		t.Errorf("segundo Save() falló: %v", err)
	}
}
