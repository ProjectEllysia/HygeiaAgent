// Package control implementa el canal de control local entre el servicio
// (hygeia-agent) y el companion de bandeja (hygeia-tray) descrito en el
// §11.4 del plan.
//
// El transporte NO es TCP: named pipe en Windows, socket Unix con permisos
// restringidos en Linux/macOS (§11.7 — la autorización viene de los permisos
// del transporte, no de confiar en "localhost", que no distingue qué usuario
// local se conecta). Encima de ese transporte se habla HTTP normal, por lo
// que el servidor es un http.Server y el cliente un http.Client con un
// DialContext propio.
//
// Superficie deliberadamente mínima (§11.7): GET /status, GET /debug y
// POST /enroll. Nada de métricas de negocio, nada de parar/arrancar el
// servicio. GET /debug (plan §12.2, Tier 3) es diagnóstico del PROCESO
// hygeia-agent en sí (goroutines, memoria, últimas líneas de log) — no
// expone nada del host ni de los activos monitorizados, así que hereda la
// misma superficie mínima sin ampliarla de verdad.
package control

import (
	"fmt"
	"regexp"
	"time"
)

// Estados que reporta el servicio al tray (§11.2).
const (
	StateConnected    = "connected"    // 🟢 el último heartbeat tuvo éxito
	StateLocalError   = "local_error"  // 🔴 no se puede recolectar, o el buffer crece
	StateUnconfigured = "unconfigured" // ⚪ no hay agentKey todavía

	// StateStarting es un cuarto estado transitorio. El §11.2 pide tres
	// estados "mínimos", y ninguno describe con honestidad el hueco entre
	// "ya tengo clave" y "ya sé si el backend responde": decir `connected`
	// ahí afirmaría un heartbeat que todavía no ha ocurrido, y
	// `local_error` sería una falsa alarma.
	StateStarting = "starting" // ⚪ configurado, sin resultado del primer envío
)

// Status es la respuesta de GET /status.
type Status struct {
	State        string    `json:"state"`
	LastPushAt   time.Time `json:"lastPushAt,omitempty"`
	BufferSize   int       `json:"bufferSize"`
	AgentVersion string    `json:"agentVersion"`
	Hostname     string    `json:"hostname,omitempty"`
	ServerURL    string    `json:"serverUrl,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
}

// DebugInfo es la respuesta de GET /debug: estado interno del proceso
// hygeia-agent para diagnóstico de campo, sin depender de encontrar el
// fichero de log (plan §12.2, Tier 3).
type DebugInfo struct {
	Goroutines int      `json:"goroutines"`
	AllocBytes uint64   `json:"allocBytes"`
	SysBytes   uint64   `json:"sysBytes"`
	NumGC      uint32   `json:"numGC"`
	RecentLog  []string `json:"recentLog,omitempty"`
}

// EnrollRequest es el cuerpo de POST /enroll.
type EnrollRequest struct {
	AgentKey string `json:"agentKey"`
}

// EnrollResponse es la respuesta de POST /enroll.
type EnrollResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// keyPattern acota el formato de la clave de agente a `keyId.secreto`
// (§11.7 / §4 del plan de backend): dos segmentos separados por un único
// punto, alfabeto opaco y longitudes acotadas. Validar el formato ANTES de
// persistir evita que un enrollment malformado deje al servicio con una
// clave que nunca va a autenticar.
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}\.[A-Za-z0-9_-]{16,128}$`)

// ValidateAgentKey comprueba el formato de la clave. No comprueba que sea
// válida contra el backend: esa autoridad, como todo lo demás, es del
// servidor — aquí solo se rechaza lo que es sintácticamente imposible.
func ValidateAgentKey(key string) error {
	if key == "" {
		return fmt.Errorf("la clave está vacía")
	}
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("formato inválido: se espera `keyId.secreto` (alfanumérico, `_` y `-`)")
	}
	return nil
}
