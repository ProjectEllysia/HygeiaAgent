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

// Status pide el estado actual al servicio (GET /status).
func (c *Client) Status(ctx context.Context) (*Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://hygeia/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("control: no se puede hablar con el servicio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control: /status devolvió %d", resp.StatusCode)
	}
	var s Status
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("control: decodificando /status: %w", err)
	}
	return &s, nil
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
	defer resp.Body.Close()

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
