// Package agent contiene el bucle principal del servicio: recolectar ->
// enviar -> drenar buffer, más el estado que el canal de control expone al
// companion de bandeja (§11.2).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/buffer"
	"github.com/ProjectEllysia/Ellysia-Hygeia/collector"
	"github.com/ProjectEllysia/Ellysia-Hygeia/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	"github.com/ProjectEllysia/Ellysia-Hygeia/shipper"
	"github.com/ProjectEllysia/Ellysia-Hygeia/version"
)

// Agent encapsula el bucle del servicio y su estado observable.
//
// Concurrencia: el bucle corre en su goroutine y el canal de control atiende
// peticiones en las suyas. Todo lo que ambos tocan (estado reportado, clave
// de agente, shipper) va bajo `mu`.
type Agent struct {
	log        *slog.Logger
	collectors []collector.Collector
	buf        *buffer.RingBuffer

	mu            sync.Mutex
	cfg           *config.Config
	shp           *shipper.Shipper
	state         string
	lastPushAt    time.Time
	lastError     string
	// lastInventory guarda el resultado del último escaneo aún no adjuntado
	// a ningún payload. collectPayload lo consume y lo limpia (envío único):
	// así el inventario completo no viaja en cada heartbeat, solo en el
	// primero tras cada ciclo de inventoryLoop.
	lastInventory *payload.Inventory
}

// New construye el agente a partir de la config ya cargada. Si la config no
// trae clave, arranca en estado "sin configurar" y el bucle no recolectará
// nada hasta que llegue un enrollment (§11.3).
func New(log *slog.Logger, cfg *config.Config) *Agent {
	a := &Agent{
		log:        log,
		collectors: collector.NewRegistry().Build(cfg.Collectors),
		buf:        buffer.NewRingBuffer(cfg.BufferPath, 1000),
		cfg:        cfg,
		state:      control.StateUnconfigured,
	}
	if cfg.IsConfigured() {
		a.shp = shipper.NewShipper(cfg.ServerURL, cfg.AgentKey)
		// Configurado, pero todavía sin heartbeat: no es "conectado" hasta
		// que el backend responda que sí.
		a.state = control.StateStarting
	}
	return a
}

// Status construye la respuesta de GET /status (§11.4).
func (a *Agent) Status() control.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	hostname, _ := os.Hostname()
	return control.Status{
		State:        a.state,
		LastPushAt:   a.lastPushAt,
		BufferSize:   a.buf.Len(),
		AgentVersion: version.Version,
		Hostname:     hostname,
		ServerURL:    a.cfg.ServerURL,
		LastError:    a.lastError,
	}
}

// Enroll persiste la clave que llega del tray y arranca el envío.
//
// Solo se acepta cuando el agente está SIN configurar (§11.7): así un
// usuario local sin privilegios no puede pisar la clave de un activo ya dado
// de alta ni reapuntar el agente. Para rotar una clave ya existente hay que
// editar la config del servicio, que tiene permisos 0600.
func (a *Agent) Enroll(agentKey string) error {
	if err := control.ValidateAgentKey(agentKey); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.IsConfigured() {
		return fmt.Errorf("el agente ya está dado de alta; para rotar la clave edita %s", a.cfg.Path())
	}

	// Persistir primero: si el guardado falla no queremos quedar con un
	// agente que envía con una clave que se perderá al reiniciar.
	previous := a.cfg.AgentKey
	a.cfg.AgentKey = agentKey
	if err := a.cfg.Save(); err != nil {
		a.cfg.AgentKey = previous
		return fmt.Errorf("no se pudo guardar la clave: %w", err)
	}

	a.shp = shipper.NewShipper(a.cfg.ServerURL, agentKey)
	// Igual que en New: dar de alta no prueba que el backend responda. El
	// primer ciclo resolverá a connected o local_error.
	a.state = control.StateStarting
	a.lastError = ""
	return nil
}

// Reset borra la clave de agente y vuelve a "sin configurar" — el
// complemento simétrico de Enroll, para cuando la clave guardada es
// inválida (revocada, mal copiada) y el agente se queda mudo sin ni
// siquiera ofrecer el enrollment, porque desde su punto de vista ya está
// "configurado" (Enroll la rechazaría). A diferencia de Enroll, Reset SÍ
// actúa sobre un agente ya configurado — es justo su propósito — pero solo
// borra la clave LOCAL: nunca la revoca en el backend, esa autoridad sigue
// siendo del servidor (§1).
func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.IsConfigured() {
		return fmt.Errorf("el agente ya está sin configurar")
	}

	previous := a.cfg.AgentKey
	a.cfg.AgentKey = ""
	if err := a.cfg.Save(); err != nil {
		a.cfg.AgentKey = previous
		return fmt.Errorf("no se pudo guardar el reset: %w", err)
	}

	a.shp = nil
	a.state = control.StateUnconfigured
	a.lastError = ""
	return nil
}

// Run ejecuta el bucle principal hasta que ctx se cancele.
func (a *Agent) Run(ctx context.Context) {
	interval := time.Duration(a.cfg.IntervalSec) * time.Second

	a.log.Info("hygeia iniciado",
		"version", version.Version,
		"interval", interval,
		"serverUrl", a.cfg.ServerURL,
		"collectors", len(a.collectors),
		"configurado", a.cfg.IsConfigured(),
	)

	if !a.sleepJitter(ctx, interval) {
		a.log.Info("cerrando agente")
		return
	}

	// Escaneo de inventario inicial síncrono (acotado por
	// inventoryScanTimeout, 30s como máximo): así el PRIMER heartbeat ya
	// lleva el inventario, sin que el usuario/backend tengan que esperar a
	// que inventoryLoop dispare su primer tick, horas más tarde.
	if a.cfg.InventoryIntervalSec > 0 {
		a.scanInventory(ctx)
	}
	go a.inventoryLoop(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	current := interval
	a.tick(ctx, &current, ticker)
	for {
		select {
		case <-ctx.Done():
			a.log.Info("cerrando agente")
			return
		case <-ticker.C:
			a.tick(ctx, &current, ticker)
		}
	}
}

// sleepJitter espera un tiempo aleatorio en [0, interval) antes del primer
// ciclo (plan §12.2, Tier 1). Sin esto, un reinicio simultáneo de una flota
// entera (corte eléctrico, actualización de Windows en todos los hosts a la
// vez) hace que todos los agentes golpeen el backend en el mismo segundo.
// Devuelve false si ctx se canceló durante la espera (arranque interrumpido).
func (a *Agent) sleepJitter(ctx context.Context, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	jitter := time.Duration(rand.Int64N(int64(interval)))
	if jitter <= 0 {
		return true
	}
	a.log.Info("esperando jitter de arranque", "duracion", jitter)
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// inventoryScanTimeout acota cuánto puede tardar un escaneo de inventario
// (enumeración de registro, bastante más lenta que los colectores de
// métricas) sin bloquear inventoryLoop indefinidamente si el SO se cuelga.
const inventoryScanTimeout = 30 * time.Second

// inventoryLoop corre en su propia goroutine, con cadencia independiente del
// heartbeat (cfg.InventoryIntervalSec, normalmente horas): escanear el
// registro es mucho más lento que los colectores de métricas y el dato
// cambia con poca frecuencia, así que atarlo al ticker principal sería tanto
// lento como derrochador. El resultado queda en a.lastInventory bajo mutex;
// collectPayload lo consume y lo limpia la próxima vez que arme un payload
// (envío único, ver payload.Payload.Inventory).
//
// Sin jitter de arranque: a diferencia del heartbeat, un escaneo de
// inventario no golpea el backend, solo el propio host, así que no hay
// "manada" que evitar. El primer escaneo lo hace Run() de forma síncrona
// antes de arrancar este loop (para garantizar que el primer heartbeat ya
// lleve inventario); este loop solo se ocupa de los escaneos siguientes.
func (a *Agent) inventoryLoop(ctx context.Context) {
	interval := time.Duration(a.cfg.InventoryIntervalSec) * time.Second
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.scanInventory(ctx)
		}
	}
}

// scanInventory ejecuta un escaneo con timeout propio (inventoryScanTimeout,
// mayor que el de los colectores de métricas porque enumerar el registro
// puede tardar más). collector.Inventory no acepta contexto, así que corre
// en su propia goroutine: si supera el timeout, scanInventory simplemente
// deja de esperarla y sigue (la goroutine huérfana termina sola y su
// resultado se descarta). Un fallo solo se loguea: se reintentará en el
// siguiente tick de inventoryLoop.
func (a *Agent) scanInventory(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, inventoryScanTimeout)
	defer cancel()

	type result struct {
		inv payload.Inventory
		err error
	}
	done := make(chan result, 1)
	go func() {
		inv, err := collector.Inventory()
		done <- result{inv, err}
	}()

	select {
	case <-cctx.Done():
		a.log.Warn("escaneo de inventario superó el timeout", "timeout", inventoryScanTimeout)
	case r := <-done:
		if r.err != nil {
			a.log.Warn("escaneo de inventario falló", "err", r.err)
			return
		}
		a.mu.Lock()
		a.lastInventory = &r.inv
		a.mu.Unlock()
	}
}

// tick ejecuta un ciclo y aplica el intervalo que sugiera el backend.
func (a *Agent) tick(ctx context.Context, current *time.Duration, ticker *time.Ticker) {
	next := a.runOnce(ctx)
	if next > 0 && next != *current {
		*current = next
		ticker.Reset(next)
		a.log.Info("intervalo auto-ajustado por backend", "intervalSec", int64(next/time.Second))
	}
}

// runOnce ejecuta un ciclo completo: recolectar -> enviar -> drenar buffer.
// Devuelve el nuevo intervalo sugerido por el backend (0 = sin cambio).
func (a *Agent) runOnce(ctx context.Context) time.Duration {
	// Sin clave no se recolecta ni se envía nada: el agente está instalado
	// pero no dado de alta contra ningún activo (§11.3). Sigue vivo para que
	// el tray pueda configurarlo.
	a.mu.Lock()
	shp := a.shp
	a.mu.Unlock()
	if shp == nil {
		return 0
	}

	p := a.collectPayload(ctx)

	resp, err := shp.Send(ctx, p)
	if err != nil {
		var permErr *shipper.PermanentError
		if errors.As(err, &permErr) {
			// El backend rechazó ESTE payload (esquema, reloj, tamaño), no
			// que esté caído: guardarlo en el buffer solo garantizaría que
			// vuelva a fallar exactamente igual más tarde, ocupando un
			// slot para siempre (el bug que motivó este tipo de error).
			a.log.Warn("envío rechazado de forma permanente, descartando payload", "err", err)
		} else {
			a.log.Warn("envío fallido, guardando en buffer", "err", err)
			if perr := a.buf.Push(p); perr != nil {
				a.log.Error("no se pudo guardar en buffer", "err", perr)
			}
		}
		a.setState(control.StateLocalError, err)
		return 0
	}
	a.log.Info("heartbeat enviado", "nextIntervalSec", resp.NextIntervalSec)
	a.setState(control.StateConnected, nil)
	a.markPush()
	a.drainBuffer(ctx, shp)
	return time.Duration(resp.NextIntervalSec) * time.Second
}

func (a *Agent) setState(state string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = state
	if err != nil {
		a.lastError = err.Error()
	} else {
		a.lastError = ""
	}
}

func (a *Agent) markPush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastPushAt = time.Now().UTC()
}

// collectPayload lanza los colectores en paralelo (goroutines) con timeout
// individual y ensambla el payload. Cada colector escribe un campo distinto
// de p.Metrics, por lo que no hace falta mutex.
func (a *Agent) collectPayload(ctx context.Context) *payload.Payload {
	p := &payload.Payload{
		AgentVersion: version.Version,
		// Truncado a microsegundo: time.Time serializa con precisión de
		// nanosegundo variable (a veces 7-9 dígitos decimales), y el
		// backend rechaza con 422 cualquier collectedAt que no tenga como
		// máximo 6 — un heartbeat podía fallar solo por cómo cayera el
		// reloj, sin relación con si el dato era válido o no.
		CollectedAt: time.Now().UTC().Truncate(time.Microsecond),
		Host:        collector.Host(),
	}

	a.mu.Lock()
	if a.lastInventory != nil {
		p.Inventory = a.lastInventory
		a.lastInventory = nil
	}
	a.mu.Unlock()

	var wg sync.WaitGroup
	for _, c := range a.collectors {
		wg.Add(1)
		go func(c collector.Collector) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := c.Collect(cctx, &p.Metrics); err != nil {
				a.log.Warn("colector falló", "name", c.Name(), "err", err)
			}
		}(c)
	}
	wg.Wait()
	return p
}

// drainBuffer envía los payloads aplazados mientras el backend responda.
func (a *Agent) drainBuffer(ctx context.Context, shp *shipper.Shipper) {
	for {
		p, err := a.buf.Pop()
		if err != nil {
			return
		}
		if _, err := shp.Send(ctx, p); err != nil {
			var permErr *shipper.PermanentError
			if errors.As(err, &permErr) {
				// Este payload en concreto nunca va a pasar (esquema, reloj
				// caducado, tamaño) — descartarlo y seguir con el resto de
				// la cola, no reencolarlo para que dé vueltas para siempre
				// (el bug real: 9 payloads de horas de antigüedad atascados
				// sin bajar nunca del buffer).
				a.log.Warn("payload en buffer rechazado de forma permanente, descartando", "err", err)
				continue
			}
			a.log.Warn("drenado interrumpido, reintentará más tarde", "err", err)
			// devolver el payload al buffer para no perderlo: reintento en el
			// próximo ciclo exitoso.
			_ = a.buf.Push(p)
			return
		}
	}
}
