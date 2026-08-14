//go:build darwin

package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

const label = "io.ellysia.hygeia-tray"

// entryPath es el LaunchAgent del usuario (~/Library/LaunchAgents), no un
// LaunchDaemon: el tray necesita la sesión de escritorio.
func entryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("autostart: localizando el home: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
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
		return fmt.Errorf("autostart: creando ~/Library/LaunchAgents: %w", err)
	}

	// La ruta del ejecutable se escapa como XML: un directorio con `&` en el
	// nombre generaría un plist corrupto si se interpolara a pelo.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
</dict>
</plist>
`, xmlEscape(label), xmlEscape(exe))

	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("autostart: escribiendo %q: %w", path, err)
	}
	return nil
}

// xmlEscape escapa un valor para incrustarlo en el plist.
func xmlEscape(s string) string {
	var buf bytes.Buffer
	// xml.EscapeText solo falla si el writer falla, y bytes.Buffer no falla.
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
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
