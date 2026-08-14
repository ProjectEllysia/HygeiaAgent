package collector

import (
	"os"
	"strings"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

const dpkgFixture = "testdata/dpkg-status"

func parseFixture(t *testing.T) []payload.Software {
	t.Helper()

	f, err := os.Open(dpkgFixture)
	if err != nil {
		t.Fatalf("abriendo %s: %v", dpkgFixture, err)
	}
	defer func() { _ = f.Close() }()

	got, err := parseDpkgStatus(f)
	if err != nil {
		t.Fatalf("parseDpkgStatus() error = %v", err)
	}
	return got
}

func find(t *testing.T, list []payload.Software, name, arch string) payload.Software {
	t.Helper()

	for _, sw := range list {
		if sw.Name == name && sw.Architecture == arch {
			return sw
		}
	}
	t.Fatalf("no se encontró %q (%s) en el inventario", name, arch)
	return payload.Software{}
}

// El fichero de estado no lista lo instalado: lista lo CONOCIDO. Esta prueba
// fija exactamente qué párrafos sobreviven, que es donde está el riesgo real
// del colector.
func TestParseDpkgStatusSelectsOnlyInstalledPackages(t *testing.T) {
	got := parseFixture(t)

	type key struct{ name, arch string }
	want := []key{
		{"libc6", "amd64"},
		{"libc6", "i386"}, // multiarch: dos instalaciones reales distintas
		{"openssh-server", "amd64"},
		// Retenido para que no se actualice, pero instalado: se cuenta. El
		// campo Status es "hold ok installed", así que un filtro que
		// comparase la cadena entera contra "install ok installed" lo
		// habría perdido.
		{"grub-common", "amd64"},
		{"base-files", "amd64"},
		// Último párrafo del fichero, sin línea en blanco detrás: comprueba
		// que el analizador cierra el párrafo pendiente al llegar al final.
		{"zlib1g", "amd64"},
	}

	if len(got) != len(want) {
		names := make([]string, 0, len(got))
		for _, sw := range got {
			names = append(names, sw.Name+":"+sw.Architecture)
		}
		t.Fatalf("se obtuvieron %d paquetes (%s), se esperaban %d",
			len(got), strings.Join(names, ", "), len(want))
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Architecture != w.arch {
			t.Errorf("paquete %d = %q (%s), se esperaba %q (%s)",
				i, got[i].Name, got[i].Architecture, w.name, w.arch)
		}
	}
}

// Los dos descartes, con nombre y motivo, porque contarlos no sería un error
// cosmético: el inventario alimenta el análisis de vulnerabilidades, y un
// paquete desinstalado seguiría produciendo hallazgos sobre software que ya
// no está en la máquina.
func TestParseDpkgStatusExcludesPackagesThatAreNotOnDisk(t *testing.T) {
	got := parseFixture(t)

	excluded := map[string]string{
		"firefox": "desinstalado sin purgar (deinstall ok config-files)",
		"nano":    "descomprimido pero sin configurar (install ok unpacked)",
	}
	for _, sw := range got {
		if why, bad := excluded[sw.Name]; bad {
			t.Errorf("%q está en el inventario y no debería: %s", sw.Name, why)
		}
	}
}

func TestParseDpkgStatusMapsTheContractFields(t *testing.T) {
	libc := find(t, parseFixture(t), "libc6", "amd64")

	want := payload.Software{
		Name:    "libc6",
		Type:    "deb",
		Vendor:  "Ubuntu Developers", // sin el correo del Maintainer
		Version: "2.39-0ubuntu8.3",
		// Installed-Size viene en KiB: 13279 * 1024.
		SizeBytes:    13279 * 1024,
		Architecture: "amd64",
		Status:       "installed",
		Source:       "dpkg",
	}
	if libc != want {
		t.Errorf("libc6 = %+v\nse esperaba %+v", libc, want)
	}
}

// Installed-Size es opcional (base-files no lo trae) y Maintainer no siempre
// lleva correo. Ni una cosa ni la otra debe descartar el paquete.
func TestParseDpkgStatusToleratesMissingOptionalFields(t *testing.T) {
	sw := find(t, parseFixture(t), "base-files", "amd64")

	if sw.SizeBytes != 0 {
		t.Errorf("SizeBytes = %d, se esperaba 0 sin Installed-Size", sw.SizeBytes)
	}
	if sw.Vendor != "Ubuntu Developers" {
		t.Errorf("Vendor = %q, se esperaba %q", sw.Vendor, "Ubuntu Developers")
	}
	if sw.Version != "13ubuntu10.2" {
		t.Errorf("Version = %q, se esperaba %q", sw.Version, "13ubuntu10.2")
	}
}

// El adaptador a Lybra descarta sin versión: "if not version: continue". Un
// paquete sin ella aparece en el informe PDF y no produce ni una sola CVE,
// así que la versión es el campo que convierte el inventario en detección.
func TestParseDpkgStatusAlwaysReportsAVersion(t *testing.T) {
	for _, sw := range parseFixture(t) {
		if sw.Version == "" {
			t.Errorf("%q va sin versión: Lybra lo descartaría entero", sw.Name)
		}
	}
}

// Las líneas de Conffiles llevan dos puntos ("/etc/ssh/sshd_config 8caefd…"
// no, pero otras rutas sí) y van indentadas. Si el analizador no saltase las
// continuaciones, contaminarían el párrafo con claves inventadas.
func TestParseDpkgStatusIgnoresContinuationLines(t *testing.T) {
	ssh := find(t, parseFixture(t), "openssh-server", "amd64")

	if ssh.Version != "1:9.6p1-3ubuntu13.5" {
		t.Errorf("Version = %q, se esperaba %q", ssh.Version, "1:9.6p1-3ubuntu13.5")
	}
	if ssh.Vendor != "Debian OpenSSH Maintainers" {
		t.Errorf("Vendor = %q, se esperaba %q", ssh.Vendor, "Debian OpenSSH Maintainers")
	}
}

// Un valor que se pase del límite del backend no cuesta esa entrada: cuesta
// el heartbeat entero con un 422 permanente.
func TestParseDpkgStatusClampsOversizedFields(t *testing.T) {
	paragraph := "Package: " + strings.Repeat("p", 600) + "\n" +
		"Status: install ok installed\n" +
		"Version: " + strings.Repeat("9", 200) + "\n" +
		"Maintainer: " + strings.Repeat("m", 400) + "\n" +
		"Architecture: " + strings.Repeat("a", 40) + "\n"

	got, err := parseDpkgStatus(strings.NewReader(paragraph))
	if err != nil {
		t.Fatalf("parseDpkgStatus() error = %v", err)
	}
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
		{"vendor", got[0].Vendor, 256},
		{"architecture", got[0].Architecture, 16},
	}
	for _, l := range limits {
		if len(l.got) > l.max {
			t.Errorf("%s ocupa %d, el backend acepta %d", l.field, len(l.got), l.max)
		}
	}
}

// Que no haya fichero de estado no es un fallo: es que la máquina no usa
// dpkg. Devolver error llenaría el log de un aviso cada seis horas en cada
// equipo que no sea Debian.
func TestDpkgInventoryIsSilentWhereDpkgDoesNotExist(t *testing.T) {
	got, err := dpkgInventory(t.TempDir() + "/no-existe/status")

	if err != nil {
		t.Errorf("dpkgInventory() error = %v, se esperaba nil", err)
	}
	if len(got) != 0 {
		t.Errorf("dpkgInventory() = %v, se esperaba vacío", got)
	}
}

func TestDpkgInventoryReadsFromDisk(t *testing.T) {
	got, err := dpkgInventory(dpkgFixture)
	if err != nil {
		t.Fatalf("dpkgInventory() error = %v", err)
	}
	if len(got) == 0 {
		t.Fatal("dpkgInventory() no devolvió nada leyendo el fichero de ejemplo")
	}
}
