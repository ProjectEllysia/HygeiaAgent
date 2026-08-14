// Package agent contiene el bucle principal del servicio: recolectar ->
// enviar -> drenar buffer, más el estado que el canal de control expone al
// companion de bandeja (§11.2).
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/buffer"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/collector"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/shipper"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/version"
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

	mu         sync.Mutex
	cfg        *config.Config
	shp        *shipper.Shipper
	state      string
	lastPushAt time.Time
	lastError  string
	// lastInventory guarda el resultado del último escaneo aún no adjuntado
	// a ningún payload. collectPayload lo consume y lo limpia (envío único):
	// así el inventario completo no viaja en cada heartbeat, solo en el
	// primero tras cada ciclo de inventoryLoop.
	//
	// Si ese payload acaba descartado por un rechazo permanente, restoreInventory
	// lo devuelve aquí: el motivo del rechazo rara vez es el inventario, y
	// perderlo obligaría a esperar horas al siguiente escaneo. En cambio, un
	// payload que va al buffer se lleva su inventario consigo y lo entrega al
	// drenar, así que ahí no hay nada que reponer.
	lastInventory *payload.Inventory
	// sentInventoryHash y sentInventoryAt identifican el último inventario que
	// el backend llegó a ACEPTAR (F-05). Mientras un escaneo nuevo dé el mismo
	// hash, no se adjunta: el campo ausente significa "no he escaneado en este
	// heartbeat" y el backend conserva el que ya tiene.
	//
	// No se persisten a disco a propósito. Tras un reinicio del servicio, el
	// hash vuelve a estar vacío y el primer escaneo se envía otra vez: repetir
	// un inventario es inofensivo —el backend lo reemplaza por uno idéntico—,
	// mientras que arrastrar un hash de una sesión anterior podría dar por
	// enviado algo que nunca llegó.
	sentInventoryHash string
	sentInventoryAt   time.Time
}

// New construye el agente a partir de la config ya cargada. Si la config no
// trae clave, arranca en estado "sin configurar" y el bucle no recolectará
// nada hasta que llegue un enrollment (§11.3).
func New(log *slog.Logger, cfg *config.Config) *Agent {
	a := &Agent{
		log:        log,
		collectors: collector.NewRegistry().Build(cfg.Collectors),
		buf:        buffer.NewRingBuffer(cfg.BufferPath, cfg.BufferMaxItems),
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
		inv := a.capInventory(r.inv)

		// F-05: si el inventario no ha cambiado desde el último que el
		// backend confirmó, no se adjunta. El campo ausente significa "no he
		// escaneado en este heartbeat" y el backend conserva el anterior, así
		// que omitirlo es exactamente equivalente a repetirlo.
		hash := inventoryHash(inv)
		if !a.shouldSendInventory(hash, time.Now()) {
			a.log.Debug("inventario sin cambios, no se adjunta",
				"aplicaciones", len(inv.Software), "hash", hash[:12])
			return
		}

		a.mu.Lock()
		a.lastInventory = &inv
		a.mu.Unlock()
	}
}

// inventoryResendInterval fuerza un envío periódico aunque nada haya cambiado.
//
// Sin él, un inventario que el backend acepta pero pierde después —una
// restauración de copia de seguridad, una migración a medias— no se volvería a
// enviar nunca, porque para el agente sigue estando "ya enviado". Un envío al
// día reconcilia solo, y sigue siendo cuatro veces menos tráfico que los
// cuatro escaneos diarios de hoy.
const inventoryResendInterval = 24 * time.Hour

// inventoryHash identifica el contenido de un inventario.
//
// Se ordenan las entradas ya serializadas antes de resumirlas, en vez de
// confiar en el orden del slice: así el hash depende solo del CONJUNTO de
// aplicaciones y no de en qué orden las devolvió el registro o el gestor de
// paquetes. Un cambio de orden sin cambio de contenido no debe parecer un
// inventario nuevo.
//
// Se resume la entrada entera, no una selección de campos: todos son estables
// entre escaneos —salen del registro o de la base de datos del gestor— y
// elegir un subconjunto solo abriría la puerta a no detectar un cambio real.
func inventoryHash(inv payload.Inventory) string {
	lines := make([]string, 0, len(inv.Software))
	for _, sw := range inv.Software {
		raw, err := json.Marshal(sw)
		if err != nil {
			// payload.Software son cadenas y enteros: no hay forma de que
			// falle. Si algún día la hubiera, mejor un hash distinto (que
			// provoca un envío de más) que uno igual (que se lo salta).
			return fmt.Sprintf("no-serializable-%d", time.Now().UnixNano())
		}
		lines = append(lines, string(raw))
	}
	sort.Strings(lines)

	h := sha256.New()
	for _, line := range lines {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// shouldSendInventory decide si un escaneo recién hecho hay que adjuntarlo.
func (a *Agent) shouldSendInventory(hash string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if hash != a.sentInventoryHash {
		return true
	}
	return now.Sub(a.sentInventoryAt) >= inventoryResendInterval
}

// markInventorySent recuerda qué inventario dio por bueno el backend.
//
// Se llama al ACEPTARSE el envío, no al adjuntarlo: si el payload se pierde o
// lo rechazan, el inventario que llevaba nunca llegó y hay que reintentarlo.
// Darlo por enviado antes de tiempo dejaría al activo con el inventario
// desactualizado hasta el reenvío forzado del día siguiente.
func (a *Agent) markInventorySent(inv *payload.Inventory, now time.Time) {
	if inv == nil {
		return
	}
	hash := inventoryHash(*inv)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.sentInventoryHash = hash
	a.sentInventoryAt = now
}

// capInventory ordena el listado por nombre y lo recorta a
// cfg.InventoryMaxItems.
//
// El recorte existe porque el backend acota el inventario
// (features.hygeia.limits.maxInventoryItems) y pasarse NO cuesta el
// inventario: cuesta el heartbeat entero, con un error de validación que el
// shipper clasifica como permanente y descarta. Como el escaneo se repite
// cada pocas horas con el mismo tamaño, ese activo perdía un heartbeat cada
// pocas horas para siempre y nunca llegaba a tener inventario.
//
// Se recorta en vez de rechazar —al revés que el backend, que sí rechaza—
// porque aquí el dato es propio, no entrada hostil: enseñar 1500 aplicaciones
// de 2100 es útil, enseñar cero no. Y se ordena antes de cortar para que el
// subconjunto visible sea el mismo entre escaneos: sin ordenar, el listado
// que sobrevive depende del orden de enumeración del registro y parpadearía
// de un escaneo a otro.
// Un escaneo sin resultados devuelve un slice nil, y un slice nil se
// serializa como `"software": null`, no como `"software": []`. El backend
// declara ese campo obligatorio y sin allow_none, así que responde 422
// "Field may not be null" y el shipper descarta el heartbeat entero, igual
// que con un inventario demasiado grande. Se normaliza aquí, que es el
// único punto por el que pasan los escaneos de los tres sistemas.
func (a *Agent) capInventory(inv payload.Inventory) payload.Inventory {
	if inv.Software == nil {
		inv.Software = []payload.Software{}
	}

	// Se ordena por nombre, versión y arquitectura, no solo por nombre.
	// sort.Slice no es estable, y el nombre por sí solo no desempata: un
	// equipo puede tener el mismo paquete en dos arquitecturas, o dos
	// versiones del mismo programa. Con un solo criterio, esas entradas
	// podían quedar en distinto orden en cada escaneo, y entonces ni el
	// recorte era el mismo entre escaneos —que es lo que promete el párrafo
	// de arriba— ni el hash de F-05 servía para detectar cambios.
	sort.Slice(inv.Software, func(i, j int) bool {
		x, y := inv.Software[i], inv.Software[j]
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		if x.Version != y.Version {
			return x.Version < y.Version
		}
		return x.Architecture < y.Architecture
	})

	a.mu.Lock()
	max := a.cfg.InventoryMaxItems
	a.mu.Unlock()

	if max > 0 && len(inv.Software) > max {
		a.log.Warn("inventario recortado al máximo configurado",
			"encontradas", len(inv.Software), "enviadas", max)
		inv.Software = inv.Software[:max]
	}
	return inv
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
		var thrErr *shipper.ThrottledError
		switch {
		case errors.As(err, &permErr):
			// El backend rechazó ESTE payload (esquema, reloj, tamaño), no
			// que esté caído: guardarlo en el buffer solo garantizaría que
			// vuelva a fallar exactamente igual más tarde, ocupando un
			// slot para siempre (el bug que motivó este tipo de error).
			a.log.Warn("envío rechazado de forma permanente, descartando payload", "err", err)
			// Con el payload se iría también el inventario que llevaba
			// adjunto, que no tiene por qué tener nada que ver con el motivo
			// del rechazo y no se volvería a escanear hasta horas después.
			// Vuelve a la cola para que lo lleve el próximo heartbeat.
			a.restoreInventory(p.Inventory)
		case errors.As(err, &thrErr):
			// El dato es bueno, solo llegó antes del suelo de cadencia: al
			// buffer, y el drenado del próximo ciclo lo entrega ya acompasado.
			// Que esto pase en el heartbeat en vivo (y no solo al drenar)
			// significa que el intervalo local va por debajo del mínimo del
			// backend; se corrige solo, porque el backend manda su
			// nextIntervalSec en cuanto acepta un envío.
			a.log.Warn("heartbeat por debajo del suelo de cadencia del backend, aplazando",
				"espera", thrErr.RetryAfter)
			if perr := a.buf.Push(p); perr != nil {
				a.log.Error("no se pudo guardar en buffer", "err", perr)
			}
		default:
			a.log.Warn("envío fallido, guardando en buffer", "err", err)
			if perr := a.buf.Push(p); perr != nil {
				a.log.Error("no se pudo guardar en buffer", "err", perr)
			}
		}
		a.setState(control.StateLocalError, err)
		return 0
	}
	// Debug y no Info: el heartbeat correcto es el caso normal, y una línea
	// cada 15 s son ~5.800 al día por equipo que no dicen nada que no diga ya
	// lastPushAt. Lo que sí se registra siempre es el CAMBIO de estado, que
	// es lo que de verdad se busca al abrir el log (ver setState).
	a.log.Debug("heartbeat enviado", "nextIntervalSec", resp.NextIntervalSec)
	a.setState(control.StateConnected, nil)
	a.markPush()
	// Aquí, y no al adjuntarlo: hasta que el backend no acepta el payload, el
	// inventario que llevaba no ha llegado a ninguna parte (F-05).
	a.markInventorySent(p.Inventory, time.Now())
	a.drainBuffer(ctx, shp)
	return time.Duration(resp.NextIntervalSec) * time.Second
}

// setState actualiza el estado observable y registra las TRANSICIONES.
//
// Solo los cambios: repetir "conectado" cada 15 segundos no informa de nada,
// mientras que "pasó de conectado a error local" a las 03:14 es exactamente
// lo que se busca al abrir el log de un agente que dio problemas.
func (a *Agent) setState(state string, err error) {
	a.mu.Lock()
	previous := a.state
	a.state = state
	if err != nil {
		a.lastError = err.Error()
	} else {
		a.lastError = ""
	}
	a.mu.Unlock()

	if previous == state {
		return
	}
	if err != nil {
		a.log.Warn("cambio de estado", "de", previous, "a", state, "err", err)
		return
	}
	a.log.Info("cambio de estado", "de", previous, "a", state)
}

// restoreInventory devuelve a la cola el inventario que viajaba en un payload
// que se acabó descartando. No pisa un escaneo más reciente: si inventoryLoop
// dejó otro mientras tanto, manda el nuevo — reponer el viejo encima sería
// retroceder.
func (a *Agent) restoreInventory(inv *payload.Inventory) {
	if inv == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastInventory == nil {
		a.lastInventory = inv
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

// maxDrainPerCycle acota cuántos payloads aplazados se envían en un mismo
// ciclo. Antes el drenado era un bucle sin límite dentro de runOnce, que a su
// vez corre en el bucle del ticker: vaciar un buffer lleno dejaba al agente
// sin recolectar ni enviar nada nuevo durante horas — para recuperar datos
// viejos se dejaba de tomar los actuales.
const maxDrainPerCycle = 5

// drainBudget es la fracción del intervalo que el drenado puede ocupar. La
// otra mitad queda libre para que el ciclo siguiente arranque a su hora.
const drainBudgetFraction = 2

// drainBuffer envía los payloads aplazados, acotado por número y por tiempo.
//
// El drenado choca de frente con el suelo de cadencia del backend (§16.2): al
// enviar los payloads uno detrás de otro, el segundo llega a cero segundos
// del primero y el backend responde 429. Antes ese 429 se trataba como fallo
// transitorio y disparaba el backoff exponencial del shipper, así que cada
// payload costaba unos 7 segundos de reintentos condenados; ahora el shipper
// lo devuelve como ThrottledError con el tiempo exacto de espera y aquí se
// espera eso, una vez, si cabe en el presupuesto del ciclo.
//
// ponytail: con un suelo de 5 s y un intervalo de 15 s esto drena en torno a
// UN payload por ciclo, así que recuperar una caída de una hora cuesta otra
// hora. Es el techo de este diseño, no un descuido: mientras la unidad de
// envío sea un heartbeat por petición, el suelo de cadencia manda. Lo levanta
// el endpoint de ingesta por lotes (A-03, opción 3), que entrega todo el
// buffer en una sola petición y no lo roza.
func (a *Agent) drainBuffer(ctx context.Context, shp *shipper.Shipper) {
	deadline := time.Now().Add(a.drainBudget())

	// Se extraen todos los del ciclo de una vez: cada extracción cuesta una
	// lectura y una reescritura del fichero, así que sacarlos de uno en uno
	// multiplicaba ese coste por el número de payloads (A-07).
	batch, err := a.buf.PopBatch(maxDrainPerCycle)
	if err != nil {
		return // buffer vacío (io.EOF) o ilegible: nada que drenar
	}

	var permErr *shipper.PermanentError
	var thrErr *shipper.ThrottledError

	for i := 0; i < len(batch); {
		if !time.Now().Before(deadline) {
			a.requeue(batch[i:])
			a.log.Debug("presupuesto de drenado agotado, sigue en el próximo ciclo",
				"enviados", i, "pendientes", a.buf.Len())
			return
		}

		_, err := shp.Send(ctx, batch[i])
		switch {
		case err == nil:
			i++

		case errors.As(err, &permErr):
			// Este payload en concreto nunca va a pasar (esquema, reloj
			// caducado, tamaño) — descartarlo y seguir con el resto de la
			// cola, no reencolarlo para que dé vueltas para siempre (el bug
			// real: 9 payloads de horas de antigüedad atascados sin bajar
			// nunca del buffer).
			a.log.Warn("payload en buffer rechazado de forma permanente, descartando", "err", err)
			i++

		case errors.As(err, &thrErr):
			// El payload es válido, solo llegó demasiado pronto. Se espera el
			// suelo y se reintenta ESTE mismo (no se incrementa i). Si la
			// espera no cabe en el presupuesto, lo que queda vuelve a la cola
			// y se sigue en el ciclo siguiente.
			if !a.waitWithin(ctx, thrErr.RetryAfter, deadline) {
				a.requeue(batch[i:])
				a.log.Debug("drenado en pausa por cadencia del backend",
					"espera", thrErr.RetryAfter, "pendientes", a.buf.Len())
				return
			}

		default:
			a.log.Warn("drenado interrumpido, reintentará más tarde", "err", err)
			a.requeue(batch[i:])
			return
		}
	}
}

// requeue devuelve al buffer los payloads que no llegaron a enviarse, para no
// perderlos.
//
// Vuelven por el final de la cola, no por donde estaban. Da igual: los
// payloads son independientes entre sí y el backend ordena la serie temporal
// por received_at, su propio reloj, no por el orden en que se los entregan.
func (a *Agent) requeue(ps []*payload.Payload) {
	for _, p := range ps {
		if err := a.buf.Push(p); err != nil {
			a.log.Error("no se pudo devolver un payload al buffer", "err", err)
		}
	}
}

// drainBudget es cuánto tiempo de este ciclo puede consumir el drenado.
func (a *Agent) drainBudget() time.Duration {
	a.mu.Lock()
	interval := time.Duration(a.cfg.IntervalSec) * time.Second
	a.mu.Unlock()
	if interval <= 0 {
		return defaultThrottleWaitFallback
	}
	return interval / drainBudgetFraction
}

// defaultThrottleWaitFallback cubre una config con intervalo no positivo, que
// config.Load ya no debería dejar pasar, pero drainBudget no puede asumirlo
// sin quedarse con un presupuesto de cero (que apagaría el drenado entero).
const defaultThrottleWaitFallback = 5 * time.Second

// waitWithin duerme `d` solo si termina antes de `deadline`. Devuelve false
// si no cabe o si el contexto se canceló: en ambos casos el caller debe
// abandonar el drenado y volver en el ciclo siguiente, no seguir esperando.
func (a *Agent) waitWithin(ctx context.Context, d time.Duration, deadline time.Time) bool {
	if d <= 0 || !time.Now().Add(d).Before(deadline) {
		return false
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
