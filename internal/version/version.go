// Package version expone la versión del agente, compartida por los dos
// binarios (hygeia-agent y hygeia-tray).
package version

// Version se envía en cada payload (campo "agentVersion" del contrato de
// ingesta) y el backend la persiste en MonitoredAsset.agent_version.
//
// La fuente de verdad de la versión que se publica es VERSION.txt, en la raíz
// del repositorio: de ahí la lee installer/build-installer.ps1 y de ahí debe
// salir el valor de los -ldflags al compilar una release.
//
//	go build -ldflags "-X github.com/ProjectEllysia/Ellysia-Hygeia/internal/version.Version=$(cat VERSION.txt)" ./cmd/hygeia-agent
//
// Este valor por defecto es deliberadamente distinto de cualquier versión
// publicada: un activo que reporte "0.1.0-dev" está corriendo un binario
// compilado a mano, y eso conviene que se vea en el panel.
var Version = "0.1.0-dev"
