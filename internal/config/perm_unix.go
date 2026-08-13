//go:build !windows

package config

import "os"

// securePath restringe el fichero al dueño (§5). En Unix el modo del
// fichero es suficiente, a diferencia de Windows, donde hay que escribir una
// DACL explícita.
func securePath(path string) error { return os.Chmod(path, 0o600) }

// secureDir restringe el directorio de estado al dueño.
func secureDir(path string) error { return os.Chmod(path, 0o700) }
