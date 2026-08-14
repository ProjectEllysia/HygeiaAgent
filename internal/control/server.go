package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"time"
)

// StatusFunc devuelve el estado actual del agente.
type StatusFunc func() Status

// EnrollFunc persiste una clave ya validada. Devuelve error si el servicio
// no acepta el enrollment (p. ej. ya está configurado, §11.7).
type EnrollFunc func(agentKey string) error

// ResetFunc limpia la clave de agente y devuelve el servicio a "sin
// configurar". Devuelve error si el servicio no acepta el reset (p. ej. ya
// estaba sin configurar).
type ResetFunc func() error

// RecentLogFunc devuelve las últimas líneas de log en memoria del proceso,
// para incluirlas en GET /debug. Puede ser nil — entonces /debug simplemente
// no trae RecentLog (plan §12.2, Tier 3).
type RecentLogFunc func() []string

// Server expone el canal de control sobre el transporte local del SO.
type Server struct {
	log       *slog.Logger
	status    StatusFunc
	enroll    EnrollFunc
	reset     ResetFunc
	recentLog RecentLogFunc
	listener  net.Listener
	http      *http.Server
}

// NewServer crea el servidor. No escucha hasta llamar a Serve. recentLog
// puede ser nil.
func NewServer(log *slog.Logger, status StatusFunc, enroll EnrollFunc, reset ResetFunc, recentLog RecentLogFunc) *Server {
	return &Server{log: log, status: status, enroll: enroll, reset: reset, recentLog: recentLog}
}

// Serve abre el transporte local y atiende peticiones hasta que ctx se
// cancele. Un fallo al abrir el canal NO debe tumbar el agente: el tray es
// opcional (§11.6), así que el caller registra el error y sigue.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := Listen()
	if err != nil {
		return err
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /debug", s.handleDebug)
	mux.HandleFunc("POST /enroll", s.handleEnroll)
	mux.HandleFunc("POST /reset", s.handleReset)

	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutCtx)
	}()

	s.log.Info("canal de control escuchando", "addr", Address())
	if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.status())
}

// handleDebug atiende GET /debug (plan §12.2, Tier 3). Diagnóstico del
// propio proceso hygeia-agent — no de las métricas que recolecta ni del
// host — pensado para que un operador (o un futuro `hygeia-agent debug`)
// pueda ver goroutines/memoria/últimas líneas de log sin buscar el fichero
// de log a mano.
func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	info := DebugInfo{
		Goroutines: runtime.NumGoroutine(),
		AllocBytes: mem.Alloc,
		SysBytes:   mem.Sys,
		NumGC:      mem.NumGC,
	}
	if s.recentLog != nil {
		info.RecentLog = s.recentLog()
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	// Cuerpo acotado: el canal solo transporta una clave, no hay motivo para
	// aceptar un cuerpo grande de un proceso local cualquiera.
	var req EnrollRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, EnrollResponse{Error: "cuerpo JSON inválido"})
		return
	}
	if err := ValidateAgentKey(req.AgentKey); err != nil {
		// La clave NUNCA se registra (§5), ni siquiera al rechazarla.
		s.log.Warn("enrollment rechazado: clave malformada")
		writeJSON(w, http.StatusBadRequest, EnrollResponse{Error: err.Error()})
		return
	}
	if err := s.enroll(req.AgentKey); err != nil {
		s.log.Warn("enrollment rechazado", "err", err)
		writeJSON(w, http.StatusConflict, EnrollResponse{Error: err.Error()})
		return
	}
	s.log.Info("enrollment aceptado, agente configurado")
	writeJSON(w, http.StatusOK, EnrollResponse{OK: true})
}

// handleReset atiende POST /reset: complemento simétrico de /enroll. Sin
// cuerpo — a diferencia del enrollment, no hay nada que el cliente deba
// enviar, solo la orden de borrar la clave local y volver a "sin
// configurar" (nunca revoca nada en el backend, eso sigue siendo autoridad
// del servidor).
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.reset(); err != nil {
		s.log.Warn("reset rechazado", "err", err)
		writeJSON(w, http.StatusConflict, ResetResponse{Error: err.Error()})
		return
	}
	s.log.Info("configuración reseteada, agente sin configurar")
	writeJSON(w, http.StatusOK, ResetResponse{OK: true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
