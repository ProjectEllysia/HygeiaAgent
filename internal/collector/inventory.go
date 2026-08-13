package collector

import (
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Inventory releva el software instalado en el equipo; la implementación
// real vive en inventory_{windows,linux,darwin}.go según el SO.
func Inventory() (payload.Inventory, error) {
	return inventory()
}
