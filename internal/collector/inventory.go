package collector

import (
	"strings"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Inventory releva el software instalado en el equipo; la implementación
// real vive en inventory_{windows,linux,darwin}.go según el SO.
func Inventory() (payload.Inventory, error) {
	return inventory()
}

// isUserSID distingue las ramas de HKEY_USERS que corresponden a una persona.
//
// Vive en el fichero común y no en inventory_windows.go porque es lógica de
// cadenas, sin nada del sistema: así sus pruebas corren en los tres sistemas
// y no solo en el runner de Windows.
//
// HKEY_USERS contiene bastante más que usuarios. Los SID que empiezan por
// S-1-5-21- son cuentas de dominio o locales reales; S-1-5-18, -19 y -20 son
// LocalSystem, LocalService y NetworkService, cuentas de servicio sin
// software propio. Y cada perfil aparece además duplicado con el sufijo
// _Classes, que es la vista de HKEY_CLASSES_ROOT de ese usuario: asociaciones
// de ficheros y componentes COM, no programas instalados.
func isUserSID(name string) bool {
	return strings.HasPrefix(name, "S-1-5-21-") && !strings.HasSuffix(name, "_Classes")
}

// dedupeSoftware quita las entradas repetidas conservando la primera.
//
// Hace falta al recorrer varias ramas del registro de Windows: un programa
// instalado para todos los usuarios aparece en la rama de la máquina, y uno
// instalado por varias personas aparece una vez por cada perfil cargado. Sin
// esto, un equipo con cuatro sesiones abiertas podría enseñar Chrome cuatro
// veces y gastar cuatro huecos del tope de inventario.
//
// La clave incluye la versión y la arquitectura a propósito: dos versiones
// distintas del mismo programa, o la de 32 y la de 64 bits, son dos
// instalaciones reales y distinguirlas importa —una puede estar al día y la
// otra no—.
func dedupeSoftware(list []payload.Software) []payload.Software {
	seen := make(map[string]struct{}, len(list))
	out := make([]payload.Software, 0, len(list))

	for _, sw := range list {
		// \x00 como separador: no puede aparecer dentro de ninguno de los
		// campos, así que "AB"+"C" y "A"+"BC" no colisionan.
		key := sw.Name + "\x00" + sw.Version + "\x00" + sw.Architecture
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sw)
	}
	return out
}
