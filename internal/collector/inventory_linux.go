package collector

import "github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"

// inventory releva el software instalado en Linux.
//
// De momento solo dpkg (Debian, Ubuntu y derivadas). En una máquina que no
// use dpkg el resultado es una lista vacía, no un error: RPM, Flatpak y Snap
// se añadirán aquí como fuentes adicionales, y cada una aporta lo que
// encuentra sin que la ausencia de las demás sea un fallo.
//
// El slice se devuelve siempre no nulo. Un slice nil se serializa como
// "software": null y el backend rechaza el heartbeat entero (ver capInventory
// en internal/agent).
func inventory() (payload.Inventory, error) {
	software, err := dpkgInventory(dpkgStatusPath)
	if err != nil {
		return payload.Inventory{Software: []payload.Software{}}, err
	}
	if software == nil {
		software = []payload.Software{}
	}
	return payload.Inventory{Software: software}, nil
}
