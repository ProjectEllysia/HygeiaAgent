package shipper

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// mustShipper construye un emisor sin ajustes de red, que es lo que quieren
// casi todas las pruebas. Los ajustes de F-09 (proxy y CA propia) tienen sus
// propias pruebas más abajo.
func mustShipper(t *testing.T, serverURL, agentKey string) *Shipper {
	t.Helper()
	s, err := NewShipper(serverURL, agentKey, Options{})
	if err != nil {
		t.Fatalf("NewShipper() error = %v", err)
	}
	return s
}

// fastBackoff acorta el backoff exponencial para que los tests de reintento
// corran en milisegundos en vez de minutos reales.
func fastBackoff(s *Shipper) {
	s.maxRetries = 2
	s.initialBackoff = 5 * time.Millisecond
	s.maxBackoff = 10 * time.Millisecond
}

func testPayload() *payload.Payload {
	return &payload.Payload{AgentVersion: "test", Host: payload.HostInfo{Hostname: "web-01"}}
}

// decodeRequestBody deshace el gzip y devuelve el payload tal como lo vería
// el backend — para comprobar que Send de verdad comprime lo que dice
// comprimir, no solo que pone la cabecera.
func decodeRequestBody(t *testing.T, r *http.Request) payload.Payload {
	t.Helper()
	if r.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, se esperaba \"gzip\"", r.Header.Get("Content-Encoding"))
	}
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatalf("el cuerpo no es gzip válido: %v", err)
	}
	defer gz.Close()
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("descomprimiendo el cuerpo: %v", err)
	}
	var p payload.Payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("el cuerpo descomprimido no es JSON válido: %v", err)
	}
	return p
}

func TestSendSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secreto123" {
			t.Errorf("Authorization = %q, se esperaba \"Bearer secreto123\"", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if r.URL.Path != "/hygeia/ingest" {
			t.Errorf("path = %q, se esperaba /hygeia/ingest", r.URL.Path)
		}

		got := decodeRequestBody(t, r)
		if got.Host.Hostname != "web-01" {
			t.Errorf("hostname recibido = %q, se esperaba \"web-01\"", got.Host.Hostname)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(IngestResponse{OK: true, NextIntervalSec: 30})
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL+"/hygeia", "secreto123")
	resp, err := s.Send(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !resp.OK || resp.NextIntervalSec != 30 {
		t.Errorf("Send() = %+v, se esperaba OK=true NextIntervalSec=30", resp)
	}
}

func TestSendRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(IngestResponse{OK: true, NextIntervalSec: 15})
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	resp, err := s.Send(context.Background(), testPayload())
	if err != nil {
		t.Fatalf("Send() error = %v, se esperaba éxito al 3er intento", err)
	}
	if resp.NextIntervalSec != 15 {
		t.Errorf("NextIntervalSec = %d, se esperaba 15", resp.NextIntervalSec)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("intentos = %d, se esperaban 3 (2 fallos + 1 éxito)", got)
	}
}

func TestSendExhaustsRetriesOnPersistentError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s) // maxRetries = 2 => 3 intentos en total

	_, err := s.Send(context.Background(), testPayload())
	if err == nil {
		t.Fatal("Send() = nil, se esperaba error tras agotar reintentos")
	}
	if got := attempts.Load(); got != int32(s.maxRetries+1) {
		t.Errorf("intentos = %d, se esperaban %d (maxRetries+1)", got, s.maxRetries+1)
	}
}

// El backoff no debe ignorar la cancelación del contexto: si el caller se
// rinde, Send debe devolver el control enseguida, no esperar a que se agoten
// los reintentos.
func TestSendRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	s.maxRetries = 100
	s.initialBackoff = 1 * time.Minute // deliberadamente largo: si el ctx no cortara, el test colgaría
	s.maxBackoff = 1 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Send(ctx, testPayload())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Send() = nil, se esperaba error por cancelación de contexto")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Send() tardó %v en devolver el control tras cancelar ctx — el backoff no lo está respetando", elapsed)
	}
}

// Un status permanente (422/400/413) no debe reintentarse: el mismo
// payload va a fallar siempre igual, así que Send debe devolver el control
// en el primer intento en vez de gastar el backoff completo.
func TestSendFailsFastOnPermanentStatus(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			s := mustShipper(t, srv.URL, "k")
			fastBackoff(s)

			_, err := s.Send(context.Background(), testPayload())
			if err == nil {
				t.Fatal("Send() = nil, se esperaba PermanentError")
			}
			var permErr *PermanentError
			if !errors.As(err, &permErr) {
				t.Fatalf("Send() error = %v (%T), se esperaba *PermanentError", err, err)
			}
			if permErr.StatusCode != code {
				t.Errorf("PermanentError.StatusCode = %d, se esperaba %d", permErr.StatusCode, code)
			}
			if got := attempts.Load(); got != 1 {
				t.Errorf("intentos = %d, se esperaba 1 (sin reintentos en error permanente)", got)
			}
		})
	}
}

// 401 (clave inválida) NO es un PermanentError: el problema es la clave, no
// el payload — tras un Reset+Enroll con clave correcta el mismo dato sí
// podría entregarse, así que debe seguir tratándose como transitorio.
// 401 sigue SIN ser un PermanentError, y por el mismo motivo de siempre: el
// payload no tiene nada de malo y con una clave válida se entregaría. Lo que
// caduca es la credencial, no el dato.
//
// Lo que sí cambió con F-03 es que ya no se reintenta. Esta prueba pedía
// antes maxRetries+1 intentos ("401 sí reintenta"); eran cuatro peticiones
// con espera exponencial contra un servidor que iba a responder lo mismo las
// cuatro veces. Ahora es un AuthError, que tiene su propia reacción en el
// agente: ni reintento, ni buffer, y estado key_rejected.
func TestSendUnauthorizedIsAuthNotPermanent(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	_, err := s.Send(context.Background(), testPayload())
	if err == nil {
		t.Fatal("Send() = nil, se esperaba error")
	}
	var permErr *PermanentError
	if errors.As(err, &permErr) {
		t.Fatal("401 se clasificó como PermanentError, no debería")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("Send() error = %T, se esperaba *AuthError", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("intentos = %d, se esperaba 1: reintentar la misma clave da el mismo 401", got)
	}
}

func TestSendMalformedResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("esto no es JSON"))
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	if _, err := s.Send(context.Background(), testPayload()); err == nil {
		t.Fatal("Send() = nil, se esperaba error al decodificar una respuesta 200 no-JSON")
	}
}

// El 429 no es "el backend está caído": es "has llegado antes de tiempo".
// Reintentarlo con backoff exponencial —lo que se hacía al tratarlo como
// transitorio— gastaba cuatro intentos que el suelo de cadencia iba a
// rechazar igual hasta que pasara el tiempo. Send debe volver a la primera.
func TestSendDoesNotRetryOnThrottle(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"details":{"min_interval_sec":5}}`))
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	_, err := s.Send(context.Background(), testPayload())

	var thr *ThrottledError
	if !errors.As(err, &thr) {
		t.Fatalf("Send() = %v, se esperaba *ThrottledError", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("el backend recibió %d peticiones, se esperaba 1 (sin reintentos)", got)
	}
	if thr.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, se esperaba 5s (de details.min_interval_sec)", thr.RetryAfter)
	}
}

// La cabecera estándar manda sobre el cuerpo: puede ponerla un proxy delante
// del backend, que es quien de verdad está cortando en ese caso.
func TestSendPrefersRetryAfterHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"details":{"min_interval_sec":5}}`))
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	_, err := s.Send(context.Background(), testPayload())

	var thr *ThrottledError
	if !errors.As(err, &thr) {
		t.Fatalf("Send() = %v, se esperaba *ThrottledError", err)
	}
	if thr.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter = %v, se esperaba 12s (cabecera Retry-After)", thr.RetryAfter)
	}
}

// Un backend antiguo (sin expose_details) o un proxy que corta por su cuenta
// responden 429 sin decir cuánto esperar. El agente no puede quedarse con
// cero: esperar nada es reintentar de inmediato, que es el bucle que se
// quería evitar.
func TestSendFallsBackToDefaultThrottleWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`sin cuerpo JSON`))
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	_, err := s.Send(context.Background(), testPayload())

	var thr *ThrottledError
	if !errors.As(err, &thr) {
		t.Fatalf("Send() = %v, se esperaba *ThrottledError", err)
	}
	if thr.RetryAfter != defaultThrottleWait {
		t.Errorf("RetryAfter = %v, se esperaba %v", thr.RetryAfter, defaultThrottleWait)
	}
}

// Un Retry-After absurdo no debe poder dormir el drenado durante horas: el
// agente acota lo que acepta del otro extremo.
func TestSendClampsAbsurdRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := mustShipper(t, srv.URL, "k")
	fastBackoff(s)

	_, err := s.Send(context.Background(), testPayload())

	var thr *ThrottledError
	if !errors.As(err, &thr) {
		t.Fatalf("Send() = %v, se esperaba *ThrottledError", err)
	}
	if thr.RetryAfter != maxThrottleWait {
		t.Errorf("RetryAfter = %v, se esperaba el tope %v", thr.RetryAfter, maxThrottleWait)
	}
}

// El 429 NO es permanente: el mismo payload sí se entregará más tarde. Si se
// clasificara como permanente, el agente lo descartaría y perdería el dato.
func TestThrottleIsNotPermanent(t *testing.T) {
	if isPermanentStatus(http.StatusTooManyRequests) {
		t.Error("429 no debe estar en isPermanentStatus: el payload es válido, solo llegó pronto")
	}
}
