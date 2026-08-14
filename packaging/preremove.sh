#!/bin/sh
# Se ejecuta ANTES de desinstalar el .deb o el .rpm.
set -e

# Solo hay que parar y desregistrar el servicio cuando el paquete se va de
# verdad. En una actualización, el gestor de paquetes también llama a este
# guion —el del paquete viejo— y hacerlo ahí dejaría el servicio parado y sin
# registrar después de un `apt upgrade` que debería ser transparente.
#
# Los dos gestores avisan de forma distinta de cuál de los dos casos es:
#   dpkg  primer argumento "remove", "purge" o "upgrade"
#   rpm   número de versiones que quedarán instaladas: 0 al desinstalar,
#         1 durante una actualización
case "$1" in
    0 | remove | purge)
        # Sin `|| true` la desinstalación fallaría en una máquina donde el
        # servicio ya se hubiera parado a mano, y dejaría el paquete a medio
        # quitar. Que ya esté parado no es un error aquí.
        hygeia-agent stop >/dev/null 2>&1 || true
        hygeia-agent uninstall >/dev/null 2>&1 || true
        echo "hygeia-agent: servicio parado y desregistrado"
        ;;
esac

# /etc/hygeia/config.toml no se toca aquí. Lleva la clave del agente, y el
# paquete lo declara como fichero de configuración para que **una
# actualización no lo pise** — que es lo que importa: sin eso, un `apt upgrade`
# dejaría al agente sin dar de alta.
#
# Al desinstalar del todo, cada gestor hace lo suyo y ninguno de los dos es un
# problema: dpkg lo conserva salvo `purge`, y rpm lo borra si nadie lo ha
# editado o lo guarda como .rpmsave si sí. Comprobado en los dos.
