package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

const testKey = "abcd1234.0123456789abcdef"

// newTestAgent construye un agente sobre un directorio temporal, con o sin
// clave, para no tocar la config real de la máquina.
func newTestAgent(t *testing.T, agentKey string) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	body := "serverUrl = \"https://ellysia.example/hygeia\"\n"
	if agentKey != "" {
		body += "agentKey = \"" + agentKey + "\"\n"
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HYGEIA_DATA_DIR", dir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(log, cfg), cfgPath
}

// Sin clave el agente arranca igualmente, en estado "sin configurar" (§11.3):
// si crasheara, el tray nunca podría configurarlo.
func TestNewWithoutKeyStartsUnconfigured(t *testing.T) {
	a, _ := newTestAgent(t, "")
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("State = %q, se esperaba %q", got, control.StateUnconfigured)
	}
}

// Con clave, el agente arranca en "iniciando": todavía no ha hablado con el
// backend, así que afirmar "conectado" sería falso.
func TestNewWithKeyStartsStarting(t *testing.T) {
	a, _ := newTestAgent(t, testKey)
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("State = %q, se esperaba %q", got, control.StateStarting)
	}
}

// Enroll persiste la clave en disco: al recargar la config debe seguir ahí,
// o el agente volvería a arrancar sin configurar tras un reinicio.
func TestEnrollPersistsKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, "")

	if err := a.Enroll(testKey); err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("tras Enroll, State = %q, se esperaba %q", got, control.StateStarting)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("recargando config: %v", err)
	}
	if reloaded.AgentKey != testKey {
		t.Errorf("clave persistida = %q, se esperaba %q", reloaded.AgentKey, testKey)
	}
}

// §11.7: un agente ya dado de alta no se puede reapuntar desde el canal de
// control. Si se pudiera, un usuario local sin privilegios podría pisar la
// clave de un activo o mandar sus métricas a otro servidor.
func TestEnrollRejectedWhenAlreadyConfigured(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	const otherKey = "wxyz9999.fedcba9876543210"
	err := a.Enroll(otherKey)
	if err == nil {
		t.Fatal("Enroll() sobre agente configurado = nil, se esperaba rechazo")
	}
	if !strings.Contains(err.Error(), "ya está dado de alta") {
		t.Errorf("error = %q, no explica el motivo del rechazo", err)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != testKey {
		t.Errorf("la clave original fue pisada: %q", reloaded.AgentKey)
	}
}

func TestEnrollRejectsMalformedKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, "")

	if err := a.Enroll("no-es-una-clave"); err == nil {
		t.Fatal("Enroll() con clave malformada = nil, se esperaba error")
	}
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("State = %q tras enrollment fallido, se esperaba %q", got, control.StateUnconfigured)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != "" {
		t.Errorf("se persistió una clave malformada: %q", reloaded.AgentKey)
	}
}

// Reset borra la clave y persiste el cambio: al recargar la config no debe
// quedar rastro de ella (§5 — nunca en logs, y tampoco superviviente en
// disco tras un reset explícito).
func TestResetClearsKey(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	if err := a.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if got := a.Status().State; got != control.StateUnconfigured {
		t.Errorf("tras Reset, State = %q, se esperaba %q", got, control.StateUnconfigured)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("recargando config: %v", err)
	}
	if reloaded.AgentKey != "" {
		t.Errorf("clave tras reset = %q, se esperaba vacía", reloaded.AgentKey)
	}
}

// Tras Reset, el agente vuelve a aceptar un Enroll (es justo el punto:
// habilitar volver a dar de alta sin tocar la config a mano).
func TestEnrollAfterReset(t *testing.T) {
	a, cfgPath := newTestAgent(t, testKey)

	if err := a.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	const newKey = "wxyz9999.fedcba9876543210"
	if err := a.Enroll(newKey); err != nil {
		t.Fatalf("Enroll() tras Reset, error = %v", err)
	}
	if got := a.Status().State; got != control.StateStarting {
		t.Errorf("tras Enroll post-reset, State = %q, se esperaba %q", got, control.StateStarting)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgentKey != newKey {
		t.Errorf("clave persistida = %q, se esperaba %q", reloaded.AgentKey, newKey)
	}
}

// Simétrico a TestEnrollRejectedWhenAlreadyConfigured: no tiene sentido
// resetear un agente que ya está sin configurar, y silenciarlo escondería
// un bug de estado en el caller (tray o test).
func TestResetRejectedWhenUnconfigured(t *testing.T) {
	a, _ := newTestAgent(t, "")

	err := a.Reset()
	if err == nil {
		t.Fatal("Reset() sobre agente sin configurar = nil, se esperaba rechazo")
	}
	if !strings.Contains(err.Error(), "ya está sin configurar") {
		t.Errorf("error = %q, no explica el motivo del rechazo", err)
	}
}

// time.Time serializa con precisión de nanosegundo variable (a veces 7-9
// dígitos decimales) — el backend rechazaba con 422 cualquier collectedAt
// con más de 6, sin relación con la validez del dato (bug real detectado en
// producción, no reproducible de forma determinista sin este truncado).
func TestCollectPayloadTruncatesCollectedAtToMicroseconds(t *testing.T) {
	a, _ := newTestAgent(t, "")
	p := a.collectPayload(context.Background())

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// El grupo de fracción es opcional: si el truncado cae justo en un
	// microsegundo múltiplo de 10^6 (raro, pero posible), el segundo sale
	// sin punto decimal — eso también es válido, no un fallo del test.
	re := regexp.MustCompile(`"collectedAt":"[^".]*(\.(\d+))?Z"`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("no se encontró collectedAt en el payload")
	}
	if len(m[2]) > 6 {
		t.Errorf("collectedAt tiene %d dígitos decimales, se esperaban ≤6: %s", len(m[2]), m[2])
	}
}

// collectPayload adjunta el inventario pendiente una sola vez: tras
// consumirlo, el siguiente payload no debe volver a traerlo. Así se evita
// mandar el listado completo de software en cada heartbeat (ver
// payload.Payload.Inventory y agent.scanInventory).
func TestCollectPayloadAttachesInventoryOnce(t *testing.T) {
	a, _ := newTestAgent(t, "")
	a.lastInventory = &payload.Inventory{
		Software: []payload.Software{{Name: "Test App"}},
	}

	first := a.collectPayload(context.Background())
	if first.Inventory == nil {
		t.Fatal("primer payload tras un escaneo: Inventory = nil, se esperaba el resultado pendiente")
	}
	if len(first.Inventory.Software) != 1 || first.Inventory.Software[0].Name != "Test App" {
		t.Errorf("Inventory.Software = %+v, no coincide con lo dejado en lastInventory", first.Inventory.Software)
	}

	a.mu.Lock()
	pending := a.lastInventory
	a.mu.Unlock()
	if pending != nil {
		t.Error("lastInventory no se limpió tras consumirlo en collectPayload")
	}

	second := a.collectPayload(context.Background())
	if second.Inventory != nil {
		t.Errorf("segundo payload sin escaneo nuevo: Inventory = %+v, se esperaba nil", second.Inventory)
	}
}

// drainBuffer debe descartar (no reencolar) un payload que el backend
// rechaza de forma permanente — si no, queda dando vueltas en el buffer sin
// poder entregarse nunca (bug real: payloads de horas de antigüedad
// atascados en producción, ver shipper.PermanentError).
func TestDrainBufferDropsPermanentlyRejectedPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	body := "serverUrl = \"" + srv.URL + "\"\nagentKey = \"" + testKey + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYGEIA_DATA_DIR", dir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(log, cfg)

	if err := a.buf.Push(&payload.Payload{AgentVersion: "test"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := a.buf.Len(); got != 1 {
		t.Fatalf("buf.Len() antes de drenar = %d, se esperaba 1", got)
	}

	a.drainBuffer(context.Background(), a.shp)

	if got := a.buf.Len(); got != 0 {
		t.Errorf("buf.Len() tras drenar un rechazo permanente = %d, se esperaba 0 (descartado, no reencolado)", got)
	}
}

// newDrainTestAgent monta un agente apuntando a `srv` con un intervalo dado,
// para poder fijar el presupuesto de drenado (drainBudget = intervalo / 2).
func newDrainTestAgent(t *testing.T, serverURL string, intervalSec int) *Agent {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	body := "serverUrl = \"" + serverURL + "\"\nagentKey = \"" + testKey + "\"\n" +
		"intervalSec = " + strconv.Itoa(intervalSec) + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYGEIA_DATA_DIR", dir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)
}

// El bug de A-03: el drenado enviaba los payloads en bucle cerrado, el
// backend respondía 429 por su suelo de cadencia, el shipper lo trataba como
// transitorio y gastaba el backoff exponencial completo — unos 7 s por
// payload — dentro del bucle del ticker. Con el buffer lleno eso dejaba al
// agente sin recolectar durante horas.
//
// Ahora el 429 llega como ThrottledError y el drenado se retira en cuanto la
// espera no cabe en el presupuesto del ciclo. Con un intervalo de 2 s el
// presupuesto es 1 s, y la espera que pide el backend (5 s) no cabe: debe
// volver de inmediato, sin dormir, y sin perder el payload.
func TestDrainBufferBacksOffOnThrottleWithoutBlocking(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"details":{"min_interval_sec":5}}`))
	}))
	defer srv.Close()

	a := newDrainTestAgent(t, srv.URL, 2) // presupuesto de drenado: 1 s

	for i := 0; i < 3; i++ {
		if err := a.buf.Push(&payload.Payload{AgentVersion: "test"}); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}

	start := time.Now()
	a.drainBuffer(context.Background(), a.shp)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("drainBuffer tardó %v; debe retirarse en cuanto la espera no cabe en el presupuesto", elapsed)
	}
	// Una sola petición: en cuanto el backend dice "espera 5 s" y eso no cabe,
	// no tiene sentido probar con el siguiente payload — el suelo es por clave
	// de agente, no por payload.
	if got := calls.Load(); got != 1 {
		t.Errorf("el backend recibió %d peticiones, se esperaba 1", got)
	}
	// Y sobre todo: no se pierde nada. Los tres siguen en el buffer.
	if got := a.buf.Len(); got != 3 {
		t.Errorf("buf.Len() = %d, se esperaba 3 (el payload aplazado vuelve a la cola)", got)
	}
}

// Con presupuesto suficiente, el drenado sí espera el suelo de cadencia y
// entrega. Es la otra mitad del contrato: acotado no significa parado.
func TestDrainBufferWaitsOutThrottleWhenBudgetAllows(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// El primer intento choca con el suelo; el segundo, ya acompasado,
		// se acepta.
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"details":{"min_interval_sec":1}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "nextIntervalSec": 15})
	}))
	defer srv.Close()

	a := newDrainTestAgent(t, srv.URL, 10) // presupuesto: 5 s, cabe la espera de 1 s

	if err := a.buf.Push(&payload.Payload{AgentVersion: "test"}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	a.drainBuffer(context.Background(), a.shp)

	if got := calls.Load(); got != 2 {
		t.Errorf("el backend recibió %d peticiones, se esperaban 2 (rechazo + entrega tras esperar)", got)
	}
	if got := a.buf.Len(); got != 0 {
		t.Errorf("buf.Len() = %d, se esperaba 0 (entregado tras respetar el suelo)", got)
	}
}

// El drenado nunca puede monopolizar el ciclo: aunque el backend acepte todo
// sin rechistar, se para en maxDrainPerCycle y deja el resto para el ciclo
// siguiente. Antes vaciaba el buffer entero en un solo tick.
func TestDrainBufferStopsAtMaxPerCycle(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "nextIntervalSec": 15})
	}))
	defer srv.Close()

	a := newDrainTestAgent(t, srv.URL, 60)

	const queued = maxDrainPerCycle + 3
	for i := 0; i < queued; i++ {
		if err := a.buf.Push(&payload.Payload{AgentVersion: "test"}); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}

	a.drainBuffer(context.Background(), a.shp)

	if got := calls.Load(); got != maxDrainPerCycle {
		t.Errorf("se enviaron %d payloads, se esperaba el tope de %d", got, maxDrainPerCycle)
	}
	if got, want := a.buf.Len(), queued-maxDrainPerCycle; got != want {
		t.Errorf("buf.Len() = %d, se esperaba %d (el resto espera al próximo ciclo)", got, want)
	}
}

// El backend acota el inventario (maxInventoryItems=2000) y pasarse no cuesta
// el inventario: cuesta el heartbeat ENTERO, con un error de validación que
// el shipper clasifica como permanente. Como el escaneo se repite cada pocas
// horas con el mismo tamaño, ese activo perdía un heartbeat cada pocas horas
// para siempre y nunca llegaba a tener inventario.
func TestCapInventoryTruncatesToTheConfiguredMaximum(t *testing.T) {
	t.Setenv("HYGEIA_INVENTORY_MAX_ITEMS", "3")
	a, _ := newTestAgent(t, testKey)

	inv := payload.Inventory{Software: []payload.Software{
		{Name: "Zulu"}, {Name: "Alfa"}, {Name: "Mike"}, {Name: "Bravo"}, {Name: "Yankee"},
	}}

	got := a.capInventory(inv)

	if len(got.Software) != 3 {
		t.Fatalf("len(Software) = %d, se esperaba 3", len(got.Software))
	}
	// Ordenado antes de cortar: sin eso, qué aplicaciones sobreviven depende
	// del orden de enumeración del registro y parpadearía entre escaneos.
	want := []string{"Alfa", "Bravo", "Mike"}
	for i, name := range want {
		if got.Software[i].Name != name {
			t.Errorf("Software[%d].Name = %q, se esperaba %q", i, got.Software[i].Name, name)
		}
	}
}

// Un inventario que ya cabe se ordena igual, pero no se toca de tamaño.
func TestCapInventoryLeavesSmallInventoriesIntact(t *testing.T) {
	a, _ := newTestAgent(t, testKey)

	inv := payload.Inventory{Software: []payload.Software{{Name: "Zulu"}, {Name: "Alfa"}}}
	got := a.capInventory(inv)

	if len(got.Software) != 2 {
		t.Fatalf("len(Software) = %d, se esperaba 2", len(got.Software))
	}
	if got.Software[0].Name != "Alfa" {
		t.Errorf("Software[0].Name = %q, se esperaba %q", got.Software[0].Name, "Alfa")
	}
}

// El inventario se adjunta a UN payload y se limpia. Si ese payload concreto
// muere por un rechazo permanente —que casi nunca tiene que ver con el
// inventario: reloj desincronizado, un porcentaje fuera de rango…— el
// inventario se iba con él y el activo se quedaba sin inventario hasta el
// siguiente escaneo, horas después.
func TestPermanentRejectRestoresTheInventoryForTheNextHeartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	a := newDrainTestAgent(t, srv.URL, 15)
	a.mu.Lock()
	a.lastInventory = &payload.Inventory{Software: []payload.Software{{Name: "7-Zip"}}}
	a.mu.Unlock()

	a.runOnce(context.Background())

	a.mu.Lock()
	restored := a.lastInventory
	a.mu.Unlock()

	if restored == nil {
		t.Fatal("lastInventory = nil tras un rechazo permanente; el inventario se perdió")
	}
	if len(restored.Software) != 1 || restored.Software[0].Name != "7-Zip" {
		t.Errorf("lastInventory = %+v, se esperaba el inventario original", restored)
	}
}

// Reponer no debe pisar un escaneo más reciente: si inventoryLoop dejó otro
// mientras el envío estaba en curso, manda el nuevo.
func TestRestoreInventoryDoesNotOverwriteANewerScan(t *testing.T) {
	a, _ := newTestAgent(t, testKey)

	fresh := &payload.Inventory{Software: []payload.Software{{Name: "nuevo"}}}
	a.mu.Lock()
	a.lastInventory = fresh
	a.mu.Unlock()

	a.restoreInventory(&payload.Inventory{Software: []payload.Software{{Name: "viejo"}}})

	a.mu.Lock()
	got := a.lastInventory
	a.mu.Unlock()
	if got != fresh {
		t.Errorf("lastInventory = %+v, se esperaba el escaneo más reciente", got)
	}
}
