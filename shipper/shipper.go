// Package shipper envía el heartbeat al backend (POST {serverUrl}/ingest)
// con gzip y reintento con backoff exponencial (README §4, §9).
package shipper

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

// Valores por defecto del reintento exponencial (README §4).
const (
	defaultMaxRetries     = 4
	defaultInitialBackoff = 1 * time.Second
	defaultMaxBackoff     = 16 * time.Second
)

// Shipper envía payloads al backend. maxRetries/initialBackoff/maxBackoff
// son campos (no constantes de paquete) para que los tests puedan acortar
// el backoff sin esperar minutos reales — NewShipper les da los valores de
// producción; shipper_test.go, al ser el mismo paquete, los sobreescribe
// directamente (plan §12.2, Tier 3).
type Shipper struct {
	serverURL string
	agentKey  string
	client    *http.Client

	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

func NewShipper(serverURL, agentKey string) *Shipper {
	return &Shipper{
		serverURL:      serverURL,
		agentKey:       agentKey,
		client:         &http.Client{Timeout: 15 * time.Second},
		maxRetries:     defaultMaxRetries,
		initialBackoff: defaultInitialBackoff,
		maxBackoff:     defaultMaxBackoff,
	}
}

// IngestResponse es la respuesta del backend (README §9).
type IngestResponse struct {
	OK              bool      `json:"ok"`
	NextIntervalSec int       `json:"nextIntervalSec"`
	ServerTime      time.Time `json:"serverTime"`
}

// PermanentError indica que el backend rechazó el PAYLOAD en sí (esquema
// inválido, reloj fuera de ventana, cuerpo demasiado grande) — no que esté
// caído o sobrecargado. Reintentar exactamente el mismo payload nunca va a
// tener éxito: el dato ya está recolectado y no puede cambiar. El caller
// (agent.drainBuffer) usa esto para descartarlo en vez de reencolarlo para
// siempre, que es justo el bug que motivó este tipo (un payload envejecido
// más allá de la ventana de reloj del backend quedaba dando vueltas en el
// buffer indefinidamente).
type PermanentError struct {
	StatusCode int
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("shipper: backend rechazó el payload de forma permanente (status %d)", e.StatusCode)
}

// ThrottledError indica que el backend rechazó el heartbeat por CADENCIA
// (429): ni está caído (transitorio) ni el payload es inválido (permanente),
// simplemente llegó antes del suelo de intervalo que el backend impone por
// clave de agente (§16.2).
//
// Es un tercer tipo porque pide una reacción distinta a los otros dos.
// Tratarlo como transitorio —lo que se hacía antes— disparaba el backoff
// exponencial: cuatro intentos que el suelo iba a rechazar igual hasta que
// pasara el tiempo, unos 7 segundos por payload gastados en peticiones
// condenadas. Lo correcto es esperar exactamente lo que el backend dice y
// volver una sola vez.
//
// El caso real que lo produce es el drenado del buffer, que envía los
// heartbeats aplazados uno detrás de otro y por definición viola el suelo.
type ThrottledError struct {
	StatusCode int
	// RetryAfter es cuánto hay que esperar antes de volver a enviar. Sale de
	// la cabecera Retry-After o, si no está, de details.min_interval_sec del
	// cuerpo. Nunca es cero: si el backend no lo dice, vale defaultThrottleWait.
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("shipper: backend rechazó el heartbeat por cadencia (status %d), esperar %s",
		e.StatusCode, e.RetryAfter)
}

// defaultThrottleWait es cuánto esperar cuando el backend responde 429 pero
// no dice cuánto (versión antigua sin expose_details, o un proxy que corta
// por su cuenta). Cinco segundos es el suelo por defecto del backend.
const defaultThrottleWait = 5 * time.Second

// maxThrottleWait acota lo que el agente acepta de un backend: un
// Retry-After absurdo no debe poder dormir el drenado durante horas.
const maxThrottleWait = 5 * time.Minute

// throttleBodyLimit acota cuánto se lee del cuerpo de un 429. Solo hace falta
// un JSON diminuto; leer sin tope dejaría al agente a merced de lo que el
// otro extremo decida mandar.
const throttleBodyLimit = 8 * 1024

// newThrottledError construye el error de cadencia leyendo cuánto hay que
// esperar. Se prueban las dos fuentes por orden de autoridad: la cabecera
// estándar Retry-After (que puede poner también un proxy por delante del
// backend) y, si no está, el details.min_interval_sec que expone la propia
// excepción de Hygeia.
func newThrottledError(resp *http.Response) *ThrottledError {
	e := &ThrottledError{StatusCode: resp.StatusCode, RetryAfter: defaultThrottleWait}

	if v := resp.Header.Get("Retry-After"); v != "" {
		// Solo la forma en segundos: la variante con fecha HTTP existe en el
		// estándar pero ningún extremo de esta ruta la emite, y aceptarla
		// obligaría a fiarse del reloj del otro lado.
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			e.RetryAfter = clampThrottleWait(time.Duration(secs) * time.Second)
			return e
		}
	}

	var body struct {
		Details struct {
			MinIntervalSec int `json:"min_interval_sec"`
		} `json:"details"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, throttleBodyLimit)).Decode(&body); err == nil {
		if body.Details.MinIntervalSec > 0 {
			e.RetryAfter = clampThrottleWait(time.Duration(body.Details.MinIntervalSec) * time.Second)
		}
	}
	return e
}

func clampThrottleWait(d time.Duration) time.Duration {
	if d > maxThrottleWait {
		return maxThrottleWait
	}
	return d
}

// isPermanentStatus identifica los códigos donde el problema es el propio
// cuerpo de la petición, no la disponibilidad del backend. 401 (clave
// inválida) queda fuera a propósito: no es el payload lo que falla, y tras
// un Reset+Enroll con una clave correcta el mismo payload sí podría
// entregarse — por eso sigue tratándose como transitorio (§7, agent.Reset).
func isPermanentStatus(code int) bool {
	switch code {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

// Send serializa el payload, lo gzip-comprime y hace POST al backend con
// Authorization: Bearer <agentKey>. Reintenta con backoff exponencial.
// Si todo falla, devuelve error para que el caller lo mande al buffer.
func (s *Shipper) Send(ctx context.Context, p *payload.Payload) (*IngestResponse, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("shipper: serializando payload: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body); err != nil {
		return nil, fmt.Errorf("shipper: gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("shipper: gzip close: %w", err)
	}

	url := s.serverURL + "/ingest"
	backoff := s.initialBackoff
	var lastErr error
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("shipper: construyendo petición: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.agentKey)
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "hygeia-agent")

		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("shipper: POST %s: %w", url, err)
		} else {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				r, derr := decodeResponse(resp.Body)
				_ = resp.Body.Close()
				if derr != nil {
					return nil, fmt.Errorf("shipper: decode respuesta: %w", derr)
				}
				return r, nil
			}
			// La cadencia se resuelve antes de cerrar el cuerpo: es de donde
			// sale cuánto hay que esperar. Sin reintentos aquí — el suelo se
			// mide contra el reloj del backend, así que insistir dentro de
			// este bucle solo gasta intentos; quien decide cuándo volver es
			// el caller, que sabe si le queda presupuesto de ciclo.
			if resp.StatusCode == http.StatusTooManyRequests {
				throttled := newThrottledError(resp)
				_ = resp.Body.Close()
				return nil, throttled
			}
			_ = resp.Body.Close()
			if isPermanentStatus(resp.StatusCode) {
				// Sin reintentos: el status ya dice que el payload nunca
				// va a pasar, reintentar solo gasta el backoff para nada.
				return nil, &PermanentError{StatusCode: resp.StatusCode}
			}
			lastErr = fmt.Errorf("shipper: backend devolvió status %d", resp.StatusCode)
		}

		// Backoff exponencial respetando cancelación del contexto.
		if attempt == s.maxRetries {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if backoff < s.maxBackoff {
			backoff *= 2
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("shipper: envío fallido")
	}
	return nil, lastErr
}

func decodeResponse(r io.ReadCloser) (*IngestResponse, error) {
	var resp IngestResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
