package shipper

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

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

	s := NewShipper(srv.URL+"/hygeia", "secreto123")
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

	s := NewShipper(srv.URL, "k")
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

	s := NewShipper(srv.URL, "k")
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

	s := NewShipper(srv.URL, "k")
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

func TestSendMalformedResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("esto no es JSON"))
	}))
	defer srv.Close()

	s := NewShipper(srv.URL, "k")
	if _, err := s.Send(context.Background(), testPayload()); err == nil {
		t.Fatal("Send() = nil, se esperaba error al decodificar una respuesta 200 no-JSON")
	}
}
