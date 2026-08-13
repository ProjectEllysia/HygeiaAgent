// Package version expone la versión del agente, compartida por los dos
// binarios (hygeia-agent y hygeia-tray).
package version

// Version se envía en cada payload (campo "agentVersion" del contrato §9).
// Se inyecta en tiempo de compilación con:
//
//	go build -ldflags "-X github.com/ProjectEllysia/Ellysia-Hygeia/version.Version=1.0.0"
var Version = "0.1.0-dev"
