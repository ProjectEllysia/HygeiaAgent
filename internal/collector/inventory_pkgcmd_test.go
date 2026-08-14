package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Salida de `rpm -qa --qf` con rpmQueryFormat, en columnas separadas por
// tabuladores. bash trae fabricante y fecha; gpg-pubkey es una clave del
// llavero de rpm, sin fabricante ni tamaño, y rpm imprime "(none)" en esos
// huecos.
const rpmSample = "bash\t5.2.26\tx86_64\tFedora Project\t8253000\t1718000000\n" +
	"gpg-pubkey\t18b8e74c\t(none)\t(none)\t0\t1717000000\n" +
	"openssh-server\t9.6p1\tx86_64\tFedora Project\t1957000\t1718100000\n"

func TestParseRPMOutputMapsTheContractFields(t *testing.T) {
	got := parseRPMOutput(rpmSample)

	if len(got) != 3 {
		t.Fatalf("se obtuvieron %d paquetes, se esperaban 3", len(got))
	}

	want := payload.Software{
		Name:    "bash",
		Type:    "rpm",
		Vendor:  "Fedora Project",
		Version: "5.2.26",
		// %{SIZE} ya viene en bytes, al revés que el Installed-Size de dpkg.
		SizeBytes:    8253000,
		InstalledAt:  time.Unix(1718000000, 0).UTC(),
		Architecture: "x86_64",
		Status:       "installed",
		Source:       "rpm",
	}
	if got[0] != want {
		t.Errorf("bash = %+v\nse esperaba %+v", got[0], want)
	}
}

// rpm imprime la cadena literal "(none)" donde no hay etiqueta. Dejarla pasar
// metería un fabricante llamado "(none)" en el informe.
func TestParseRPMOutputTranslatesTheNonePlaceholder(t *testing.T) {
	got := parseRPMOutput(rpmSample)

	key := got[1]
	if key.Name != "gpg-pubkey" {
		t.Fatalf("el segundo paquete es %q, se esperaba gpg-pubkey", key.Name)
	}
	if key.Vendor != "" {
		t.Errorf("Vendor = %q, se esperaba vacío", key.Vendor)
	}
	if key.Architecture != "" {
		t.Errorf("Architecture = %q, se esperaba vacío", key.Architecture)
	}
}

// Una línea a medias —salida truncada por el timeout, por ejemplo— no debe
// colarse como un paquete con los campos corridos.
func TestParseRPMOutputSkipsIncompleteLines(t *testing.T) {
	got := parseRPMOutput("bash\t5.2.26\tx86_64\n\n\t\t\t\t\t\n")

	if len(got) != 0 {
		t.Errorf("se obtuvieron %d paquetes de una salida incompleta, se esperaban 0: %+v", len(got), got)
	}
}

// Salida de `flatpak list --app --columns=application,version,arch,origin`.
const flatpakSample = "org.mozilla.firefox\t128.0\tx86_64\tflathub\n" +
	"org.gimp.GIMP\t2.10.38\tx86_64\tflathub\n"

func TestParseFlatpakOutputMapsTheContractFields(t *testing.T) {
	got := parseFlatpakOutput(flatpakSample)

	if len(got) != 2 {
		t.Fatalf("se obtuvieron %d aplicaciones, se esperaban 2", len(got))
	}
	want := payload.Software{
		Name:         "org.mozilla.firefox",
		Type:         "flatpak",
		Vendor:       "flathub",
		Version:      "128.0",
		Architecture: "x86_64",
		Status:       "installed",
		Source:       "flatpak",
	}
	if got[0] != want {
		t.Errorf("firefox = %+v\nse esperaba %+v", got[0], want)
	}
}

// Unas versiones de flatpak imprimen cabecera con --columns y otras no. Sin
// el descarte aparecería una aplicación llamada "Application".
func TestParseFlatpakOutputSkipsTheHeaderWhenThereIsOne(t *testing.T) {
	withHeader := "Application\tVersion\tArch\tOrigin\n" + flatpakSample

	got := parseFlatpakOutput(withHeader)

	if len(got) != 2 {
		t.Fatalf("se obtuvieron %d aplicaciones, se esperaban 2", len(got))
	}
	for _, sw := range got {
		if strings.EqualFold(sw.Name, "Application") {
			t.Error("la cabecera de la tabla se coló como una aplicación")
		}
	}
}

// Salida de `snap list`: una tabla con cabecera y columnas separadas por
// espacios. El editor lleva pegado un distintivo cuando está verificado.
const snapSample = `Name               Version          Rev    Tracking       Publisher   Notes
core22             20240111         1122   latest/stable  canonical✓  base
firefox            122.0-2          3836   latest/stable  mozilla✓    -
hello-world        6.4              29     latest/stable  canonical✓  -
`

func TestParseSnapOutputMapsTheContractFields(t *testing.T) {
	got := parseSnapOutput(snapSample)

	if len(got) != 3 {
		t.Fatalf("se obtuvieron %d snaps, se esperaban 3", len(got))
	}
	want := payload.Software{
		Name:    "core22",
		Type:    "snap",
		Vendor:  "canonical", // sin el distintivo de editor verificado
		Version: "20240111",
		Status:  "installed",
		Source:  "snap",
	}
	if got[0] != want {
		t.Errorf("core22 = %+v\nse esperaba %+v", got[0], want)
	}
}

func TestParseSnapOutputSkipsTheHeader(t *testing.T) {
	for _, sw := range parseSnapOutput(snapSample) {
		if sw.Name == "Name" {
			t.Error("la cabecera de la tabla se coló como un snap")
		}
	}
}

// `snap list` sin ningún snap instalado no imprime tabla, sino un aviso.
//
// Hoy ese aviso sale por la salida de error y no llega hasta aquí, pero el
// analizador no debe depender de eso: sin exigir la cabecera, "No snaps are
// installed yet." se convertiría en un paquete llamado "No", versión "snaps".
func TestParseSnapOutputWithNothingInstalled(t *testing.T) {
	outputs := map[string]string{
		"salida vacía": "",
		"aviso de snapd por la salida estándar": "No snaps are installed yet. " +
			"Try 'snap install hello-world'.\n",
	}
	for name, out := range outputs {
		if got := parseSnapOutput(out); len(got) != 0 {
			t.Errorf("%s: se obtuvieron %d snaps: %+v", name, len(got), got)
		}
	}
}

// Los tres analizadores deben aguantar una salida vacía, que es lo que
// devuelve runPkgCommand cuando el gestor no está instalado en la máquina —el
// caso normal, no el excepcional.
func TestParsersToleranteAnAbsentPackageManager(t *testing.T) {
	parsers := map[string]func(string) []payload.Software{
		"rpm":     parseRPMOutput,
		"flatpak": parseFlatpakOutput,
		"snap":    parseSnapOutput,
	}
	for name, parse := range parsers {
		if got := parse(""); len(got) != 0 {
			t.Errorf("%s devolvió %d entradas con la salida vacía", name, len(got))
		}
	}
}

// Que el gestor no exista no es un fallo: es que esta máquina no lo usa.
func TestRunPkgCommandIsSilentWhenTheToolIsMissing(t *testing.T) {
	out, err := runPkgCommand("hygeia-gestor-que-no-existe", "--version")

	if err != nil {
		t.Errorf("runPkgCommand() error = %v, se esperaba nil", err)
	}
	if out != "" {
		t.Errorf("runPkgCommand() = %q, se esperaba vacío", out)
	}
}

// Mismo criterio de acotado que en dpkg: pasarse de los límites del contrato
// en un solo campo cuesta el heartbeat entero con un 422 permanente.
func TestParseRPMOutputClampsOversizedFields(t *testing.T) {
	line := strings.Repeat("p", 600) + "\t" + strings.Repeat("9", 200) + "\t" +
		strings.Repeat("a", 40) + "\t" + strings.Repeat("v", 400) + "\t0\t0\n"

	got := parseRPMOutput(line)

	if len(got) != 1 {
		t.Fatalf("se obtuvieron %d paquetes, se esperaba 1", len(got))
	}
	limits := []struct {
		field string
		got   string
		max   int
	}{
		{"name", got[0].Name, 512},
		{"version", got[0].Version, 128},
		{"architecture", got[0].Architecture, 16},
		{"vendor", got[0].Vendor, 256},
	}
	for _, l := range limits {
		if len(l.got) > l.max {
			t.Errorf("%s ocupa %d, el backend acepta %d", l.field, len(l.got), l.max)
		}
	}
}
