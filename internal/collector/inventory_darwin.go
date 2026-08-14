package collector

import "github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"

// inventory en macOS sigue sin implementar (F-01): habría que recorrer los
// paquetes .app leyendo su Info.plist, y consultar Homebrew si está.
//
// Devuelve una lista vacía, no nula: un slice nulo se serializa como
// "software": null y el backend rechaza el heartbeat entero. capInventory ya
// lo normaliza, pero conviene no depender de ello desde aquí.
func inventory() (payload.Inventory, error) {
	return payload.Inventory{Software: []payload.Software{}}, nil
}
