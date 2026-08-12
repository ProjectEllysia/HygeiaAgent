package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

// maxIntervalSec acota intervalSec por arriba (24h) — ver Load.
const maxIntervalSec = 86400

// minInventoryIntervalSec y maxInventoryIntervalSec acotan el escaneo de
// inventario: por abajo para que un typo no machaque el registro en cada
// tick, por arriba (7 días) para que igual siga refrescándose solo sin
// intervención manual.
const (
	minInventoryIntervalSec = 300
	maxInventoryIntervalSec = 604800
)

// Cotas de bufferMaxItems (cuántos heartbeats aplazados caben en disco).
//
// El default de 1000 son unas 4 h de histórico con intervalSec=15. No se sube
// más por ahora aunque el backend ya acepte hasta 24 h de backfill
// (maxBackfillSec): el ring buffer reescribe el fichero entero en cada
// operación, así que su coste crece con el CUADRADO del número de elementos.
// Subir el tope antes de que el buffer sea lineal (A-07) cambiaría un
// problema de pérdida de datos por uno de entrada/salida.
const (
	defaultBufferMaxItems = 1000
	maxBufferMaxItems     = 100000 // ~300 MB a 3 KB por payload
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
	// InventoryIntervalSec controla la cadencia del escaneo de software,
	// independiente de IntervalSec (README plan: el inventario no debería
	// enviarse en cada heartbeat). A diferencia de IntervalSec, el backend
	// no puede ajustarlo vía nextIntervalSec: es solo config local.
	InventoryIntervalSec int `toml:"inventoryIntervalSec"`

	// BufferMaxItems es cuántos heartbeats aplazados retiene el buffer en
	// disco cuando el backend no responde. Deja de estar escrito a fuego en
	// agent.New porque el valor correcto depende del despliegue: un servidor
	// de producción con una ventana de mantenimiento larga y un portátil que
	// se suspende cada noche no tienen por qué guardar lo mismo.
	//
	// El techo real no lo pone este número, sino la ventana de backfill del
	// backend (features.hygeia.limits.maxBackfillSec): un payload más viejo
	// que eso se rechaza al drenar, por muy bien guardado que estuviera.
	BufferMaxItems int `toml:"bufferMaxItems"`

	// path recuerda de dónde se cargó, para que Save() reescriba el mismo
	// fichero sin que el caller tenga que arrastrar la ruta.
	path string `toml:"-"`
}

// DataDir es el directorio de estado del servicio: config, buffer y (en el
// caso del canal de control por loopback) el fichero de token. El servicio
// corre como LocalSystem/root con un working directory que no controlamos
// (en Windows, System32), así que nunca se usan rutas relativas.
func DataDir() string {
	if v := os.Getenv("HYGEIA_DATA_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "Hygeia")
	case "darwin":
		return "/Library/Application Support/Hygeia"
	default:
		return "/etc/hygeia"
	}
}

// DefaultPath es la ruta del fichero de config del servicio.
func DefaultPath() string {
	if v := os.Getenv("HYGEIA_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(DataDir(), "config.toml")
}

// Load lee el fichero de config TOML y aplica los overrides de entorno.
// Si el fichero no existe no es fatal: la config puede venir íntegramente
// de entorno.
//
// La ausencia de agentKey TAMPOCO es fatal (§11.3): el servicio arranca en
// estado "sin configurar" y espera a que el tray le haga enrollment por el
// canal de control. Un servicio que crashea sin clave no podría ser
// configurado nunca desde la bandeja.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	c := &Config{
		IntervalSec:          15,
		BufferPath:           filepath.Join(DataDir(), "buffer.jsonl"),
		Collectors:           []string{"cpu", "memory", "disk", "network", "processes"},
		InventoryIntervalSec: 21600, // 6h: el software instalado cambia poco
		BufferMaxItems:       defaultBufferMaxItems,
		path:                 path,
	}

	if data, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("config: parseando %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: leyendo %q: %w", path, err)
	}

	if err := applyEnv(c); err != nil {
		return nil, err
	}
	if c.IntervalSec <= 0 {
		c.IntervalSec = 15
	}
	// Tope defensivo: un typo tipo "intervalSec = 1500000" no debería dejar
	// el agente mudo durante semanas (plan §12.2, Tier 2) — el activo
	// dejaría de reportar y nadie lo notaría hasta mucho después.
	if c.IntervalSec > maxIntervalSec {
		c.IntervalSec = maxIntervalSec
	}
	if c.InventoryIntervalSec <= 0 {
		c.InventoryIntervalSec = 21600
	}
	if c.InventoryIntervalSec < minInventoryIntervalSec {
		c.InventoryIntervalSec = minInventoryIntervalSec
	}
	if c.InventoryIntervalSec > maxInventoryIntervalSec {
		c.InventoryIntervalSec = maxInventoryIntervalSec
	}
	if c.BufferMaxItems <= 0 {
		c.BufferMaxItems = defaultBufferMaxItems
	}
	// Tope defensivo, del mismo tipo que el de intervalSec: un typo tipo
	// "bufferMaxItems = 10000000" llenaría el disco del activo justo cuando
	// el backend está caído y nadie lo está mirando.
	if c.BufferMaxItems > maxBufferMaxItems {
		c.BufferMaxItems = maxBufferMaxItems
	}
	if c.ServerURL == "" {
		return nil, fmt.Errorf("config: serverUrl es obligatorio (fichero %q o HYGEIA_SERVER_URL)", path)
	}
	return c, nil
}

// Path devuelve el fichero del que se cargó esta config.
func (c *Config) Path() string { return c.path }

// IsConfigured indica si el agente tiene clave y por tanto puede recolectar
// y enviar. Sin clave, el bucle principal se queda quieto (§11.3).
func (c *Config) IsConfigured() bool { return c.AgentKey != "" }

// Save persiste la config al fichero del que se cargó, con permisos
// restringidos (§5: la clave de agente nunca queda legible por otros
// usuarios locales). Reescribe el fichero completo: los comentarios del
// TOML original no se conservan.
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("config: no hay ruta asociada, no se puede guardar")
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: creando directorio: %w", err)
	}
	if err := secureDir(dir); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: serializando: %w", err)
	}

	// Escritura atómica: fichero temporal, se restringe ANTES de que lleve el
	// nombre definitivo, y luego rename sobre el destino. Restringir después
	// del rename dejaría una ventana en la que la clave está en su sitio
	// final y todavía legible.
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: escribiendo temporal: %w", err)
	}
	if err := securePath(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: renombrando a %q: %w", c.path, err)
	}
	return nil
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
	if v := os.Getenv("HYGEIA_INVENTORY_INTERVAL_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: HYGEIA_INVENTORY_INTERVAL_SEC inválido: %w", err)
		}
		c.InventoryIntervalSec = n
	}
	if v := os.Getenv("HYGEIA_BUFFER_MAX_ITEMS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: HYGEIA_BUFFER_MAX_ITEMS inválido: %w", err)
		}
		c.BufferMaxItems = n
	}
	return nil
}
