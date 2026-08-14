// Package shipper envía el heartbeat al backend (POST {serverUrl}/ingest)
// con gzip y reintento con backoff exponencial (README §4, §9).
package shipper

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
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

// clientTimeout acota una petición completa, incluido el cuerpo.
const clientTimeout = 15 * time.Second

// Options son los ajustes de red para entornos corporativos (F-09). Los dos
// campos son opcionales y lo normal es que vengan vacíos.
type Options struct {
	// ProxyURL fuerza un proxy concreto. Vacío deja el comportamiento por
	// defecto de Go, que ya respeta HTTP_PROXY/HTTPS_PROXY/NO_PROXY.
	ProxyURL string
	// CAFile es una autoridad de certificación adicional en PEM.
	CAFile string
}

// NewShipper construye el emisor. Devuelve error si los ajustes de red son
// inválidos —un proxy mal escrito, un fichero de CA que no existe o que no
// contiene certificados— en vez de arrancar con una configuración a medio
// aplicar: si alguien ha puesto un caFile, conectar sin él no es "funcionar",
// es fallar más tarde y con un error de certificado que no menciona la causa.
func NewShipper(serverURL, agentKey string, opts Options) (*Shipper, error) {
	client, err := newHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	return &Shipper{
		serverURL:      serverURL,
		agentKey:       agentKey,
		client:         client,
		maxRetries:     defaultMaxRetries,
		initialBackoff: defaultInitialBackoff,
		maxBackoff:     defaultMaxBackoff,
	}, nil
}

func newHTTPClient(opts Options) (*http.Client, error) {
	// Sin ajustes, no se construye transporte propio: el de Go ya respeta las
	// variables de entorno de proxy y el almacén del sistema, y clonarlo para
	// no cambiar nada solo añadiría superficie donde equivocarse.
	if opts.ProxyURL == "" && opts.CAFile == "" {
		return &http.Client{Timeout: clientTimeout}, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	if opts.ProxyURL != "" {
		proxy, err := url.Parse(opts.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("shipper: proxyUrl inválido %q: %w", opts.ProxyURL, err)
		}
		// url.Parse acepta casi cualquier cosa: "proxy.empresa.local:3128"
		// se analiza sin error como esquema "proxy.empresa.local" y opaco
		// "3128", y luego el proxy simplemente no se usa, en silencio.
		if proxy.Scheme == "" || proxy.Host == "" {
			return nil, fmt.Errorf(
				"shipper: proxyUrl %q no lleva esquema y host; se esperaba algo como http://proxy.empresa.local:3128",
				opts.ProxyURL)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}

	if opts.CAFile != "" {
		pool, err := caPool(opts.CAFile)
		if err != nil {
			return nil, err
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	return &http.Client{Timeout: clientTimeout, Transport: transport}, nil
}

// caPool devuelve el almacén de certificados del sistema MÁS el de path.
//
// Aditivo, nunca sustitutivo, y esto es lo importante de la función: poner
// RootCAs con solo el certificado corporativo dejaría al agente sin confiar
// en ninguna autoridad pública. Funcionaría mientras el servidor estuviera
// detrás de la inspección TLS de la empresa y dejaría de funcionar en cuanto
// no lo estuviera —un portátil fuera de la oficina, por ejemplo— con un error
// de certificado que nadie relacionaría con esta línea.
func caPool(path string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("shipper: leyendo el almacén de certificados del sistema: %w", err)
	}

	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("shipper: leyendo caFile: %w", err)
	}
	// AppendCertsFromPEM no devuelve error, devuelve false si no añadió
	// nada. Sin comprobarlo, apuntar caFile a un fichero DER, a un PEM
	// truncado o a un fichero de texto cualquiera se aceptaría en silencio y
	// el fallo aparecería después como un error de certificado.
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf(
			"shipper: caFile %q no contiene ningún certificado PEM válido (¿está en formato DER?)", path)
	}
	return pool, nil
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
