#!/bin/sh
# Se ejecuta tras instalar el .deb o el .rpm.
set -e

# El servicio se registra con el PROPIO binario en vez de traer aquí una
# unidad de systemd escrita a mano.
#
# Es a propósito: dos definiciones de la misma unidad acabarían divergiendo, y
# entonces un agente instalado desde el paquete se comportaría distinto que uno
# instalado con `hygeia-agent install`. La del binario ya trae el
# Restart=always que evita que un agente caído deje el activo mudo, que es
# justo lo que no se quiere perder por una copia desactualizada.
#
# En una actualización el servicio ya existe y `install` falla. No es un
# problema y no debe tumbar la instalación del paquete.
# Se distingue "ya existía" de un fallo de verdad, en vez de tragarse la
# salida y anunciar lo mismo en los dos casos. La primera versión de este
# guion hacía eso, y en una prueba anunció "el servicio ya estaba registrado"
# en una instalación limpia donde `install` había fallado por otro motivo: el
# mensaje tapaba justo el dato que hacía falta.
if install_output=$(hygeia-agent install 2>&1); then
    echo "hygeia-agent: servicio registrado"
elif echo "$install_output" | grep -qi "already exists"; then
    echo "hygeia-agent: el servicio ya estaba registrado"
else
    # No se aborta la instalación del paquete por esto: los ficheros están
    # bien puestos y el problema se arregla con una orden. Pero se dice.
    echo "hygeia-agent: AVISO — no se pudo registrar el servicio:"
    echo "  $install_output"
    echo "  Regístralo a mano con: sudo hygeia-agent install"
fi

# Tras una actualización hay un binario nuevo en disco pero el proceso en
# marcha sigue siendo el viejo, y nadie se entera: el activo reporta la
# versión antigua indefinidamente, hasta el próximo reinicio de la máquina.
#
# try-restart es exactamente lo que hace falta y no otra cosa: reinicia el
# servicio SI estaba en marcha, y no hace nada si estaba parado. Arrancarlo
# aquí sin más pondría en marcha un agente que alguien había parado a
# propósito.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl try-restart hygeia-agent.service >/dev/null 2>&1 || true
fi

cat <<'EOF'

hygeia-agent instalado. Quedan dos pasos:

  1. Pon la URL de tu Ellysia en /etc/hygeia/config.toml
  2. Arranca el servicio y dale su clave:

       sudo systemctl start hygeia-agent
       echo "$CLAVE_DE_AGENTE" | sudo hygeia-agent enroll

Para comprobar que está reportando:

       hygeia-agent info

EOF
