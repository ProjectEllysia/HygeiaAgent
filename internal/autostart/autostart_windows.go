package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// runKey es la clave de arranque por usuario. HKCU, no HKLM: registrar el
// tray no debe pedir elevación (el servicio, que sí la pide, es otro
// binario y otro mecanismo).
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("autostart: localizando el ejecutable: %w", err)
	}
	// Resolver symlinks: si el binario se movió, la entrada del registro
	// debe apuntar a la ruta real, no a un enlace que puede desaparecer.
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

func enable() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: abriendo la clave Run: %w", err)
	}
	defer func() { _ = k.Close() }()
	// Entrecomillado: la ruta casi siempre contiene espacios
	// (C:\Program Files\…) y sin comillas el shell la partiría.
	if err := k.SetStringValue(AppName, `"`+exe+`"`); err != nil {
		return fmt.Errorf("autostart: escribiendo la entrada de arranque: %w", err)
	}
	return nil
}

func disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("autostart: abriendo la clave Run: %w", err)
	}
	defer func() { _ = k.Close() }()
	if err := k.DeleteValue(AppName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("autostart: borrando la entrada de arranque: %w", err)
	}
	return nil
}

func enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("autostart: abriendo la clave Run: %w", err)
	}
	defer func() { _ = k.Close() }()
	if _, _, err := k.GetStringValue(AppName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("autostart: leyendo la entrada de arranque: %w", err)
	}
	return true, nil
}
