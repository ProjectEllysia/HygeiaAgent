package collector

import (
	"errors"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// linuxSources son las fuentes de inventario que se consultan, en orden.
//
// Se consultan todas siempre: no hay una forma fiable de deducir la
// distribución, y preguntar es barato porque cada fuente comprueba primero si
// su gestor existe en la máquina. Una Debian responde por dpkg y snap, una
// Fedora por rpm y flatpak, y las que no aplican no cuestan nada.
//
// No se deduplica entre fuentes a propósito. Un mismo programa instalado por
// dos gestores distintos —Firefox como paquete y como snap, por ejemplo— son
// dos instalaciones reales, en rutas distintas y con versiones que pueden no
// coincidir. Ocultar una escondería la que estuviera sin actualizar.
var linuxSources = []func() ([]payload.Software, error){
	func() ([]payload.Software, error) { return dpkgInventory(dpkgStatusPath) },
	rpmInventory,
	flatpakInventory,
	snapInventory,
}

// inventory releva el software instalado en Linux.
//
// El slice se devuelve siempre no nulo: un slice nil se serializa como
// "software": null y el backend rechaza el heartbeat entero (ver capInventory
// en internal/agent).
func inventory() (payload.Inventory, error) {
	software := []payload.Software{}
	var errs []error

	for _, scan := range linuxSources {
		found, err := scan()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		software = append(software, found...)
	}

	// Un fallo solo se reporta si nos hemos quedado sin NADA que enviar.
	//
	// El agente descarta el escaneo completo cuando esta función devuelve
	// error, así que informar de un fallo parcial tiraría también lo que sí
	// se pudo leer. Y los fallos parciales son casi siempre benignos: `snap
	// list` termina con código de error cuando no hay ningún snap instalado,
	// que no es un problema del que haya que enterarse cada seis horas.
	//
	// Lo que sí importa —una base de datos de RPM corrupta en una máquina que
	// solo usa RPM— deja el inventario vacío y ahí sí sale el error.
	if len(software) == 0 && len(errs) > 0 {
		return payload.Inventory{Software: software}, errors.Join(errs...)
	}
	return payload.Inventory{Software: software}, nil
}
