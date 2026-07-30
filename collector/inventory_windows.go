package collector

import (
	"strconv"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

// uninstallPaths cubre las tres ubicaciones donde Windows registra software instalado [web:68]

var uninstallPaths = []struct {
	root registry.Key
	path string
	arch string
}{
	{
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		"x64",
	},
	{
		registry.LOCAL_MACHINE,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
		"x86",
	},
	{
		registry.CURRENT_USER,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		"x64",
	},
}

func inventory() (payload.Inventory, error) {
	installedSoftware, err := getInstalledSoftware()
	if err != nil {
		return payload.Inventory{}, err
	}

	return payload.Inventory{
		Software: installedSoftware,
	}, nil
}

func getInstalledSoftware() ([]payload.Software, error) {
	var result []payload.Software

	for _, up := range uninstallPaths {
		key, err := registry.OpenKey(
			up.root,
			up.path,
			registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE,
		)
		if err != nil {
			continue
		}

		subKeyNames, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}

		for _, guid := range subKeyNames {
			sw, ok := readSoftwareEntry(up.root, up.path, guid, up.arch)
			if ok {
				result = append(result, sw)
			}
		}
	}

	return result, nil
}

func readSoftwareEntry(root registry.Key, basePath, guid, arch string) (payload.Software, bool) {
	fullPath := basePath + `\` + guid
	entryKey, err := registry.OpenKey(root, fullPath, registry.QUERY_VALUE)
	if err != nil {
		return payload.Software{}, false
	}
	defer entryKey.Close()

	name, _, err := entryKey.GetStringValue("DisplayName")
	if err != nil || name == "" {
		// Muchas subclaves son componentes internos sin DisplayName; se descartan [web:68]
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

	sw := payload.Software{
		Name:         name,
		Type:         softwareType,
		Vendor:       vendor,
		Version:      version,
		GUID:         guid,
		InstalledAt:  parseRegistryDate(installDateRaw),
		InstallPath:  installPath,
		Architecture: arch,
		SizeBytes:    estimatedSize * 1024, // EstimatedSize viene en KB [web:69]
		Status:       "installed",
		Source:       "registry",
	}

	return sw, true
}

// parseRegistryDate convierte el formato YYYYMMDD de InstallDate a time.Time [web:69]
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
