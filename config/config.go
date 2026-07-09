package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config es la configuración del agente. Se carga de un fichero TOML
// (config.toml) con override por variables de entorno. Cada campo del
// fichero se mapea por nombre (ver config.example.toml).
type Config struct {
	ServerURL   string   `toml:"serverUrl"`
	AgentKey    string   `toml:"agentKey"`
	IntervalSec int      `toml:"intervalSec"`
	Collectors  []string `toml:"collectors"`
	BufferPath  string   `toml:"bufferPath"`
}

// Load lee el fichero de config (TODO: parse TOML) y aplica los overrides
// de entorno. Devuelve error si faltan los campos obligatorios.
func Load(path string) (*Config, error) {
	c := &Config{
		IntervalSec: 15,
		BufferPath:  "hygeia-buffer.jsonl",
		Collectors:  []string{"cpu", "memory", "disk", "network", "processes"},
	}

	// TODO: parsear el fichero TOML en `path` con, p. ej.:
	//   github.com/pelletier/go-toml/v2  ->  toml.Unmarshal(data, c)
	// Mientras tanto, la config se entrega por variables de entorno.

	if err := applyEnv(c); err != nil {
		return nil, err
	}
	if c.ServerURL == "" {
		return nil, fmt.Errorf("config: serverUrl es obligatorio (fichero %q o HYGEIA_SERVER_URL)", path)
	}
	if c.AgentKey == "" {
		return nil, fmt.Errorf("config: agentKey es obligatorio (fichero %q o HYGEIA_AGENT_KEY)", path)
	}
	return c, nil
}

func applyEnv(c *Config) error {
	if v := os.Getenv("HYGEIA_SERVER_URL"); v != "" {
		c.ServerURL = v
	}
	if v := os.Getenv("HYGEIA_AGENT_KEY"); v != "" {
		c.AgentKey = v
	}
	if v := os.Getenv("HYGEIA_INTERVAL_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: HYGEIA_INTERVAL_SEC inválido: %w", err)
		}
		c.IntervalSec = n
	}
	if v := os.Getenv("HYGEIA_BUFFER_PATH"); v != "" {
		c.BufferPath = v
	}
	return nil
}
