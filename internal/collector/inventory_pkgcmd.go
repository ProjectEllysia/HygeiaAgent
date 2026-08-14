package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Fuentes de inventario que se consultan ejecutando el gestor de paquetes,
// en vez de leyendo sus ficheros como se hace con dpkg.
//
// Para RPM es la opción sensata: su base de datos es binaria y el formato ha
// cambiado de motor con los años (Berkeley DB, luego sqlite), así que leerla a
// mano ataría el agente a una versión concreta. Flatpak y Snap directamente no
// exponen otra cosa.
//
// Igual que con dpkg, el análisis vive aparte de la ejecución y este fichero
// no lleva sufijo de plataforma: así las pruebas del análisis corren en los
// tres sistemas y no solo en el runner de Ubuntu.

// pkgCommandTimeout acota cada consulta a un gestor de paquetes.
//
// Hace falta de verdad: `rpm -qa` se queda esperando el cerrojo de su base de
// datos si hay una transacción en curso —un `dnf update` a medias, por
// ejemplo—. Y collector.Inventory no acepta contexto: el agente lo lanza en su
// propia goroutine y la abandona a los 30 s, pero el proceso hijo seguiría
// vivo indefinidamente. Con CommandContext, el timeout lo mata.
const pkgCommandTimeout = 10 * time.Second

// runPkgCommand ejecuta un gestor de paquetes y devuelve su salida.
//
// Que el programa no esté instalado NO es un error: es que esta máquina no usa
// ese gestor, que es el caso normal (una Debian no tiene rpm, una Fedora no
// tiene dpkg). Devuelve salida vacía y sigue.
func runPkgCommand(name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), pkgCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	// LC_ALL=C fija el idioma de la salida: snap y flatpak traducen sus
	// cabeceras, y el análisis las reconoce por su texto en inglés.
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("inventario: ejecutando %q: %w", name, err)
	}
	return string(out), nil
}

// -----------------------------------------------------------------------
// RPM — Fedora, RHEL, CentOS, openSUSE
// -----------------------------------------------------------------------

// rpmQueryFormat pide solo los campos del contrato de ingesta.
//
// Se pide %{VERSION} y no %{VERSION}-%{RELEASE} a propósito: RELEASE es el
// número de empaquetado de la distribución ("5.fc39"), no la versión del
// programa. Los rangos de CVE del NVD se expresan contra la versión de
// origen, que es lo que %{VERSION} devuelve.
const rpmQueryFormat = `%{NAME}\t%{VERSION}\t%{ARCH}\t%{VENDOR}\t%{SIZE}\t%{INSTALLTIME}\n`

func rpmInventory() ([]payload.Software, error) {
	out, err := runPkgCommand("rpm", "-qa", "--qf", rpmQueryFormat)
	if err != nil {
		return nil, err
	}
	return parseRPMOutput(out), nil
}

func parseRPMOutput(out string) []payload.Software {
	var res []payload.Software

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			continue
		}
		name := rpmValue(fields[0])
		if name == "" {
			continue
		}
		res = append(res, payload.Software{
			Name:         clampField(name, 512),
			Type:         "rpm",
			Vendor:       clampField(rpmValue(fields[3]), 256),
			Version:      clampField(rpmValue(fields[1]), 128),
			InstalledAt:  rpmInstallTime(fields[5]),
			Architecture: clampField(rpmValue(fields[2]), 16),
			// A diferencia de dpkg, %{SIZE} ya viene en bytes.
			SizeBytes: parseUint(rpmValue(fields[4])),
			Status:    "installed",
			Source:    "rpm",
		})
	}
	return res
}

// rpmValue traduce el "(none)" que rpm imprime cuando una etiqueta no existe.
// Dejarlo pasar metería la cadena literal "(none)" como nombre de fabricante.
func rpmValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "(none)" {
		return ""
	}
	return v
}

func rpmInstallTime(v string) time.Time {
	secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}
	}
	return time.Unix(secs, 0).UTC()
}

// -----------------------------------------------------------------------
// Flatpak
// -----------------------------------------------------------------------

// Se piden solo las aplicaciones (--app), no los entornos de ejecución.
// Un runtime como org.freedesktop.Platform declara su versión como rama
// ("23.08"), no como versión de un programa, así que no sirve para casar un
// CPE y solo añadiría ruido al informe.
func flatpakInventory() ([]payload.Software, error) {
	out, err := runPkgCommand("flatpak", "list", "--app",
		"--columns=application,version,arch,origin")
	if err != nil {
		return nil, err
	}
	return parseFlatpakOutput(out), nil
}

func parseFlatpakOutput(out string) []payload.Software {
	var res []payload.Software

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		// Algunas versiones de flatpak imprimen cabecera con --columns y
		// otras no; sin este descarte aparecería una aplicación llamada
		// "Application" en el inventario.
		if name == "" || strings.EqualFold(name, "Application") {
			continue
		}
		res = append(res, payload.Software{
			Name:    clampField(name, 512),
			Type:    "flatpak",
			Vendor:  clampField(strings.TrimSpace(fields[3]), 256), // el remoto: flathub…
			Version: clampField(strings.TrimSpace(fields[1]), 128),
			// La arquitectura de flatpak es la del sistema (x86_64,
			// aarch64), no la del paquete.
			Architecture: clampField(strings.TrimSpace(fields[2]), 16),
			Status:       "installed",
			Source:       "flatpak",
		})
	}
	return res
}

// -----------------------------------------------------------------------
// Snap
// -----------------------------------------------------------------------

// snap list no tiene salida en formato de máquina, así que se analiza la
// tabla. Las columnas son Name, Version, Rev, Tracking, Publisher y Notes,
// separadas por espacios, con una cabecera.
func snapInventory() ([]payload.Software, error) {
	out, err := runPkgCommand("snap", "list")
	if err != nil {
		return nil, err
	}
	return parseSnapOutput(out), nil
}

func parseSnapOutput(out string) []payload.Software {
	var res []payload.Software

	// Solo se leen filas DESPUÉS de la cabecera, en vez de saltársela y
	// tragar todo lo demás.
	//
	// Sin snaps instalados, snap responde "No snaps are installed yet. Try
	// 'snap install hello-world'." y no imprime tabla. Hoy ese texto sale por
	// la salida de error y cmd.Output() no lo recoge, pero apoyarse en qué
	// flujo eligió snapd es frágil: si algún día saliera por la salida
	// estándar, esa frase se convertiría en un paquete llamado "No" con
	// versión "snaps". Exigir la cabecera lo descarta pase lo que pase.
	seenHeader := false

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if name == "Name" {
			seenHeader = true
			continue
		}
		if !seenHeader {
			continue
		}

		sw := payload.Software{
			Name:    clampField(name, 512),
			Type:    "snap",
			Version: clampField(fields[1], 128),
			Status:  "installed",
			Source:  "snap",
			// snap list no informa de la arquitectura.
		}
		if len(fields) >= 5 {
			// El editor lleva un distintivo pegado cuando está verificado
			// ("canonical✓", "mozilla✓"), que no forma parte del nombre.
			sw.Vendor = clampField(strings.TrimRight(fields[4], "✓*+"), 256)
		}
		res = append(res, sw)
	}
	return res
}

func parseUint(v string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
