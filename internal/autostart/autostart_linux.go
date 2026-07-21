//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

// entryPath es el fichero .desktop de autostart XDG del usuario.
func entryPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("autostart: localizando el home: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "hygeia-tray.desktop"), nil
}

func enable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("autostart: localizando el ejecutable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	path, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("autostart: creando ~/.config/autostart: %w", err)
	}

	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Hygeia
Comment=Estado del agente de monitorizacion Hygeia
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, exe)
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("autostart: escribiendo %q: %w", path, err)
	}
	return nil
}

func disable() error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("autostart: borrando %q: %w", path, err)
	}
	return nil
}

func enabled() (bool, error) {
	path, err := entryPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
