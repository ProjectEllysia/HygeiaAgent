package collector

import (
	"strconv"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Windows registra el software desinstalable en una clave "Uninstall" que
// existe por duplicado —la vista de 64 bits y la de 32 en WOW6432Node— y una
// vez por cada ámbito de instalación: el de la máquina y el de cada usuario.
const (
	uninstallKey64 = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	uninstallKey32 = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
)

type uninstallBranch struct {
	root registry.Key
	path string
	arch string
}

func inventory() (payload.Inventory, error) {
	software := getInstalledSoftware()
	if software == nil {
		software = []payload.Software{}
	}
	return payload.Inventory{Software: software}, nil
}

func getInstalledSoftware() []payload.Software {
	var result []payload.Software

	for _, branch := range uninstallBranches() {
		key, err := registry.OpenKey(
			branch.root,
			branch.path,
			registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
		)
		if err != nil {
			// Lo normal: no todos los usuarios tienen software propio, y
			// una rama que no existe no es un fallo.
			continue
		}

		subKeyNames, err := key.ReadSubKeyNames(-1)
		_ = key.Close()
		if err != nil {
			continue
		}

		for _, guid := range subKeyNames {
			if sw, ok := readSoftwareEntry(branch, guid); ok {
				result = append(result, sw)
			}
		}
	}

	return dedupeSoftware(result)
}

// uninstallBranches enumera todas las claves Uninstall que hay que recorrer.
//
// La versión anterior usaba HKEY_CURRENT_USER para el ámbito de usuario, y
// eso no funcionaba: hygeia-agent corre como servicio bajo LocalSystem, así
// que HKEY_CURRENT_USER es la rama de LocalSystem —vacía—, no la del usuario
// sentado delante del equipo. Todo el software instalado "solo para este
// usuario" (navegadores, clientes de mensajería, herramientas de desarrollo:
// buena parte de lo que instala alguien sin privilegios de administrador)
// quedaba fuera del inventario.
//
// Se enumera HKEY_USERS, a la que el servicio sí tiene acceso por correr con
// privilegios elevados.
//
// Limitación conocida: HKEY_USERS solo contiene los perfiles CARGADOS, es
// decir, los de las sesiones abiertas. El software de un usuario que no ha
// iniciado sesión desde el último arranque no aparece. Cargar su NTUSER.DAT a
// mano con RegLoadKey sería invasivo —modifica el registro de la máquina para
// leerlo— y no compensa para un escaneo periódico.
func uninstallBranches() []uninstallBranch {
	// La rama WOW6432Node solo existe en sistemas de 64 bits. Su ausencia es
	// la forma más barata de saber que la clave principal es de 32.
	sixtyFourBit := keyExists(registry.LOCAL_MACHINE, uninstallKey32)

	nativeArch := "x86"
	if sixtyFourBit {
		nativeArch = "x64"
	}

	branches := []uninstallBranch{
		{registry.LOCAL_MACHINE, uninstallKey64, nativeArch},
	}
	if sixtyFourBit {
		branches = append(branches, uninstallBranch{registry.LOCAL_MACHINE, uninstallKey32, "x86"})
	}

	for _, sid := range userSIDs() {
		branches = append(branches, uninstallBranch{registry.USERS, sid + `\` + uninstallKey64, nativeArch})
		if sixtyFourBit {
			branches = append(branches, uninstallBranch{registry.USERS, sid + `\` + uninstallKey32, "x86"})
		}
	}

	return branches
}

func keyExists(root registry.Key, path string) bool {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_ = key.Close()
	return true
}

func userSIDs() []string {
	names, err := registry.USERS.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var out []string
	for _, name := range names {
		if isUserSID(name) {
			out = append(out, name)
		}
	}
	return out
}

func readSoftwareEntry(branch uninstallBranch, guid string) (payload.Software, bool) {
	entryKey, err := registry.OpenKey(branch.root, branch.path+`\`+guid, registry.QUERY_VALUE)
	if err != nil {
		return payload.Software{}, false
	}
	defer func() { _ = entryKey.Close() }()

	name, _, err := entryKey.GetStringValue("DisplayName")
	if err != nil || name == "" {
		// Muchas subclaves son componentes internos sin DisplayName.
		return payload.Software{}, false
	}

	vendor, _, _ := entryKey.GetStringValue("Publisher")
	version, _, _ := entryKey.GetStringValue("DisplayVersion")
	installPath, _, _ := entryKey.GetStringValue("InstallLocation")
	installDateRaw, _, _ := entryKey.GetStringValue("InstallDate")
	estimatedSize, _, _ := entryKey.GetIntegerValue("EstimatedSize")
	windowsInstaller, _, _ := entryKey.GetIntegerValue("WindowsInstaller")

	softwareType := "EXE"
	if windowsInstaller == 1 {
		softwareType = "MSI"
	}

	return payload.Software{
		Name:         clampField(name, 512),
		Type:         softwareType,
		Vendor:       clampField(vendor, 256),
		Version:      clampField(version, 128),
		GUID:         clampField(guid, 128),
		InstalledAt:  parseRegistryDate(installDateRaw),
		InstallPath:  clampField(installPath, 1024),
		Architecture: branch.arch,
		SizeBytes:    estimatedSize * 1024, // EstimatedSize viene en KB
		Status:       "installed",
		Source:       "registry",
	}, true
}

// parseRegistryDate convierte el formato YYYYMMDD de InstallDate a time.Time.
func parseRegistryDate(raw string) time.Time {
	if len(raw) != 8 {
		return time.Time{}
	}
	year, err1 := strconv.Atoi(raw[0:4])
	month, err2 := strconv.Atoi(raw[4:6])
	day, err3 := strconv.Atoi(raw[6:8])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}
