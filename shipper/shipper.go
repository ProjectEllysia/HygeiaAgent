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
				resp.Body.Close()
				if derr != nil {
					return nil, fmt.Errorf("shipper: decode respuesta: %w", derr)
				}
				return r, nil
			}
			resp.Body.Close()
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
