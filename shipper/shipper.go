// Package shipper envía el heartbeat al backend (POST {serverUrl}/ingest)
// con gzip y reintento con backoff exponencial (README §4, §9).
package shipper

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type Shipper struct {
	serverURL string
	agentKey  string
	client    *http.Client
}

func NewShipper(serverURL, agentKey string) *Shipper {
	return &Shipper{
		serverURL: serverURL,
		agentKey:  agentKey,
		client:    &http.Client{Timeout: 10 * time.Second},
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
	// TODO:
	//   1. json.Marshal(p)
	//   2. gzip.NewWriter -> bytes comprimidos
	//   3. http.NewRequestWithContext(ctx, POST, s.serverURL+"/ingest", body)
	//      req.Header.Set("Authorization", "Bearer "+s.agentKey)
	//      req.Header.Set("Content-Encoding", "gzip")
	//      req.Header.Set("Content-Type", "application/json")
	//   4. bucle de reintentos con backoff (1s, 2s, 4s...) respetando ctx
	//   5. json.NewDecoder(resp.Body).Decode(&r)  (Status 2xx)
	_ = ctx
	_ = p
	return nil, errors.New("shipper: Send no implementado")
}
