// Package autostart registra el companion de bandeja para que arranque con
// la sesión de escritorio del usuario (§11.1).
//
// Es deliberadamente distinto del servicio: hygeia-agent lo gestiona el
// gestor de servicios del SO (kardianos/service), que arranca antes del
// login y bajo una cuenta de sistema. El tray, en cambio, necesita una
// sesión de escritorio, así que se registra en el mecanismo de arranque
// *por usuario* de cada plataforma:
//
//	Windows  HKCU\...\CurrentVersion\Run
//	Linux    ~/.config/autostart/hygeia-tray.desktop (XDG)
//	macOS    ~/Library/LaunchAgents/…plist
//
// Todo por usuario: registrar el tray no requiere privilegios de
// administrador, a diferencia de instalar el servicio.
package autostart

// AppName es el identificador del tray en el mecanismo de arranque del SO.
const AppName = "HygeiaTray"

// Enable registra el ejecutable actual para arrancar con la sesión.
// Idempotente: llamarlo dos veces deja el mismo resultado.
func Enable() error { return enable() }

// Disable elimina el registro. No es error que no estuviera registrado.
func Disable() error { return disable() }

// Enabled indica si el tray está registrado para arrancar con la sesión.
func Enabled() (bool, error) { return enabled() }
