package main

// AgentVersion se envía en cada payload (campo "agentVersion" del contrato §9).
// Se inyecta en tiempo de compilación con:
//   go build -ldflags "-X main.AgentVersion=1.0.0"
const AgentVersion = "0.1.0-dev"
