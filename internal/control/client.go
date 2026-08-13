package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client habla con el canal de control del servicio desde el tray.
type Client struct {
	http *http.Client
}

// NewClient construye el cliente sobre el transporte local del SO. El host
// de la URL ("hygeia") es un placeholder: DialContext ignora la dirección y
// va siempre al pipe/socket local.
func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Transport: &http.Transport{DialContext: dial},
			Timeout:   5 * time.Second,
		},
	}
}

// getJSON hace GET a `path` sobre el canal de control y decodifica la
// respuesta en `out`. Status y Debug comparten este mismo patrón.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hygeia"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("control: no se puede hablar con el servicio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("control: %s devolvió %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("control: decodificando %s: %w", path, err)
	}
	return nil
}

// Status pide el estado actual al servicio (GET /status).
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var s Status
	if err := c.getJSON(ctx, "/status", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Debug pide el diagnóstico interno del proceso (GET /debug, plan §12.2
// Tier 3): goroutines, memoria y últimas líneas de log.
func (c *Client) Debug(ctx context.Context) (*DebugInfo, error) {
	var d DebugInfo
	if err := c.getJSON(ctx, "/debug", &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Enroll envía la clave de agente al servicio (POST /enroll). La valida
// antes de mandarla para dar feedback inmediato en la UI sin gastar un
// viaje al servicio; el servicio la revalida igualmente (§11.7: no confiar
// en que el cliente validó).
func (c *Client) Enroll(ctx context.Context, agentKey string) error {
	if err := ValidateAgentKey(agentKey); err != nil {
		return err
	}
	body, err := json.Marshal(EnrollRequest{AgentKey: agentKey})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://hygeia/enroll", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("control: no se puede hablar con el servicio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var er EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return fmt.Errorf("control: respuesta ilegible del servicio (status %d)", resp.StatusCode)
	}
	if !er.OK {
		if er.Error == "" {
			er.Error = fmt.Sprintf("el servicio rechazó el enrollment (status %d)", resp.StatusCode)
		}
		return fmt.Errorf("%s", er.Error)
	}
	return nil
}

// Reset pide al servicio que borre su clave de agente y vuelva a "sin
// configurar" (POST /reset) — para cuando la clave configurada es inválida
// o se quiere dar de alta el activo de nuevo, sin editar la config a mano.
func (c *Client) Reset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://hygeia/reset", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("control: no se puede hablar con el servicio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rr ResetResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return fmt.Errorf("control: respuesta ilegible del servicio (status %d)", resp.StatusCode)
	}
	if !rr.OK {
		if rr.Error == "" {
			rr.Error = fmt.Sprintf("el servicio rechazó el reset (status %d)", resp.StatusCode)
		}
		return fmt.Errorf("%s", rr.Error)
	}
	return nil
}
