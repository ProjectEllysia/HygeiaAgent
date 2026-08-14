package collector

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Este fichero no lleva sufijo de plataforma a propósito, aunque dpkg solo
// exista en Linux: el análisis del fichero de estado es texto puro y no toca
// el sistema, así que sin sufijo las pruebas corren también en Windows y
// macOS. Quien decide dónde se usa es inventory_linux.go.

// dpkgStatusPath es la base de datos de paquetes de dpkg en Debian, Ubuntu y
// derivadas.
//
// Se lee el fichero directamente en vez de enlazar una librería o invocar
// dpkg-query. El formato es de cabeceras estilo correo electrónico (RFC 822):
// párrafos separados por líneas en blanco, "Clave: valor", y continuaciones
// indentadas. Analizarlo cabe en este fichero, no añade dependencia, y no
// depende de que haya un binario en el PATH de la cuenta del servicio.
const dpkgStatusPath = "/var/lib/dpkg/status"

// dpkgInventory lee el software instalado según dpkg.
//
// Que el fichero no exista NO es un error: significa que la máquina no usa
// dpkg (Fedora, Alpine, macOS…). Devolver error ahí llenaría el log de un
// aviso cada seis horas en cada equipo que no sea Debian.
func dpkgInventory(path string) ([]payload.Software, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inventario dpkg: abriendo %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	return parseDpkgStatus(f)
}

// parseDpkgStatus analiza el formato de /var/lib/dpkg/status.
func parseDpkgStatus(r io.Reader) ([]payload.Software, error) {
	var out []payload.Software
	fields := make(map[string]string, 24)

	flush := func() {
		if sw, ok := dpkgSoftware(fields); ok {
			out = append(out, sw)
		}
		clear(fields)
	}

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.TrimSpace(line) == "":
			// Fin de párrafo: un paquete completo.
			flush()
		case line[0] == ' ' || line[0] == '\t':
			// Continuación de un campo multilínea (Description, Conffiles).
			// No interesa ninguno, y saltárselas aquí evita que una línea
			// como " /etc/ssh/sshd_config abc123" de Conffiles se confunda
			// con una cabecera por llevar dos puntos.
		default:
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			// En minúsculas porque el formato declara las claves
			// insensibles a mayúsculas. dpkg escribe siempre la forma
			// canónica, pero equivocarse aquí no daría un inventario
			// parcial: daría uno vacío, en silencio.
			fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("inventario dpkg: leyendo el fichero de estado: %w", err)
	}
	// El último párrafo puede no acabar en línea en blanco.
	flush()

	return out, nil
}

// dpkgSoftware traduce un párrafo ya analizado a una entrada del inventario,
// o lo descarta.
func dpkgSoftware(fields map[string]string) (payload.Software, bool) {
	name := fields["package"]
	if name == "" || !dpkgIsInstalled(fields["status"]) {
		return payload.Software{}, false
	}

	return payload.Software{
		Name: clampField(name, 512),
		Type: "deb",
		// Maintainer, sin el correo: "Ubuntu Developers <ubuntu-devel@…>"
		// se queda en "Ubuntu Developers".
		Vendor:       clampField(dpkgMaintainerName(fields["maintainer"]), 256),
		Version:      clampField(fields["version"], 128),
		Architecture: clampField(fields["architecture"], 16),
		SizeBytes:    dpkgInstalledSize(fields["installed-size"]),
		Status:       "installed",
		Source:       "dpkg",
	}, true
}

// dpkgIsInstalled decide si un paquete está de verdad en el disco.
//
// El campo Status son tres palabras: estado deseado, marca de error y estado
// real ("install ok installed"). El que importa es el TERCERO. El fichero no
// lista lo instalado, lista lo conocido, e incluye lo desinstalado sin purgar
// ("deinstall ok config-files") y lo descomprimido a medio configurar
// ("install ok unpacked").
//
// Contarlos sería peor que no informar: el inventario alimenta el análisis de
// vulnerabilidades con Lybra, así que un paquete borrado hace meses seguiría
// generando hallazgos sobre software que no está en la máquina.
//
// Mirar el tercer campo y no la cadena entera también acierta con
// "hold ok installed": un paquete retenido para que no se actualice sigue
// instalado, y comparar contra "install ok installed" lo habría perdido.
func dpkgIsInstalled(status string) bool {
	parts := strings.Fields(status)
	return len(parts) == 3 && parts[2] == "installed"
}

func dpkgMaintainerName(v string) string {
	if i := strings.IndexByte(v, '<'); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// dpkgInstalledSize convierte el campo Installed-Size, que viene en KiB.
func dpkgInstalledSize(v string) uint64 {
	kib, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return kib * 1024
}

// clampField recorta un valor al máximo que acepta el backend.
//
// Pasarse de largo en un solo campo de una sola aplicación no cuesta esa
// entrada: cuesta el heartbeat entero, con un 422 que el shipper clasifica
// como rechazo permanente. Estos valores salen de un fichero que el agente no
// controla, así que se acotan en el origen (mismo criterio que capInventory
// con el número de entradas).
func clampField(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// El corte por bytes puede partir un carácter multibyte por la mitad;
	// ToValidUTF8 se lleva el resto suelto. Recortar en bytes deja siempre
	// menos caracteres que el máximo, que es el lado seguro.
	return strings.ToValidUTF8(s[:limit], "")
}
