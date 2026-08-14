package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateAgentKey(t *testing.T) {
	valid := []string{
		"abcd1234.0123456789abcdef",
		"KEY_id-01.SECRETO-con_guiones-y-largo-suficiente",
		strings.Repeat("k", 64) + "." + strings.Repeat("s", 128),
	}
	for _, k := range valid {
		if err := ValidateAgentKey(k); err != nil {
			t.Errorf("ValidateAgentKey(%q) = %v, se esperaba nil", k, err)
		}
	}

	invalid := map[string]string{
		"vacía":               "",
		"sin punto":           "abcd1234012345678900",
		"keyId corto":         "abc.0123456789abcdef",
		"secreto corto":       "abcd1234.demasiadocort",
		"dos puntos":          "abcd1234.0123456789abcdef.extra",
		"espacio":             "abcd1234.0123456789 abcdef",
		"caracteres hostiles": "abcd1234.0123456789abcdef; rm -rf /",
		"keyId largo":         strings.Repeat("k", 65) + ".0123456789abcdef",
		"salto de línea":      "abcd1234.0123456789abcdef\nX",
	}
	for name, k := range invalid {
		if err := ValidateAgentKey(k); err == nil {
			t.Errorf("%s: ValidateAgentKey(%q) = nil, se esperaba error", name, k)
		}
	}
}

// newTestServer arranca el canal de control sobre el transporte real del SO
// (named pipe / socket Unix) y devuelve un cliente conectado a él.
func newTestServer(t *testing.T, status StatusFunc, enroll EnrollFunc) *Client {
	t.Helper()
	return newTestServerWithReset(t, status, enroll, func() error { return nil })
}

func newTestServerWithReset(t *testing.T, status StatusFunc, enroll EnrollFunc, reset ResetFunc) *Client {
	t.Helper()
	return newTestServerWithLog(t, status, enroll, reset, nil)
}

func newTestServerWithLog(t *testing.T, status StatusFunc, enroll EnrollFunc, reset ResetFunc, recentLog RecentLogFunc) *Client {
	t.Helper()
	isolate(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(log, status, enroll, reset, recentLog)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
			t.Error("el servidor no cerró a tiempo")
		}
	})

	// Esperar a que el transporte esté aceptando conexiones.
	client := NewClient()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// Si Serve ya ha fallado, no tiene sentido seguir sondeando tres
		// segundos para acabar diciendo "no llegó a aceptar conexiones": ese
		// mensaje genérico escondía el error real —el bind del socket
		// fallando por una ruta demasiado larga— y costó varias iteraciones
		// de CI averiguar qué pasaba de verdad.
		select {
		case err := <-errCh:
			// Devolverlo: el t.Cleanup de arriba también espera en este
			// canal, y dejarlo vacío convertiría un fallo claro en dos
			// confusos ("el servidor no cerró a tiempo").
			errCh <- err
			t.Fatalf("el canal de control no pudo arrancar: %v", err)
		default:
		}

		cctx, ccancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := client.Status(cctx)
		ccancel()
		if err == nil {
			return client
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("el canal de control no llegó a aceptar conexiones (Serve no reportó error)")
	return nil
}

func TestStatusRoundTrip(t *testing.T) {
	want := Status{
		State:        StateConnected,
		BufferSize:   7,
		AgentVersion: "9.9.9",
		Hostname:     "web-01",
	}
	client := newTestServer(t,
		func() Status { return want },
		func(string) error { return nil },
	)

	got, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got.State != want.State || got.BufferSize != want.BufferSize ||
		got.AgentVersion != want.AgentVersion || got.Hostname != want.Hostname {
		t.Errorf("Status() = %+v, se esperaba %+v", got, want)
	}
}

func TestEnrollHappyPath(t *testing.T) {
	var received string
	client := newTestServer(t,
		func() Status { return Status{State: StateUnconfigured} },
		func(key string) error { received = key; return nil },
	)

	const key = "abcd1234.0123456789abcdef"
	if err := client.Enroll(context.Background(), key); err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if received != key {
		t.Errorf("el servicio recibió %q, se esperaba %q", received, key)
	}
}

// Una clave malformada no debe llegar siquiera a la función de enrollment:
// el servicio valida el formato antes de persistir nada (§11.7).
func TestEnrollRejectsMalformedKey(t *testing.T) {
	called := false
	client := newTestServer(t,
		func() Status { return Status{State: StateUnconfigured} },
		func(string) error { called = true; return nil },
	)

	if err := client.Enroll(context.Background(), "no-es-una-clave"); err == nil {
		t.Fatal("Enroll() con clave malformada = nil, se esperaba error")
	}
	if called {
		t.Error("el enrollment se ejecutó con una clave malformada")
	}
}

// El error del servicio (p. ej. "ya está dado de alta") debe llegar al tray
// con su texto, que es lo que se muestra al usuario en el diálogo.
func TestEnrollPropagatesServiceError(t *testing.T) {
	client := newTestServer(t,
		func() Status { return Status{State: StateConnected} },
		func(string) error { return errors.New("el agente ya está dado de alta") },
	)

	err := client.Enroll(context.Background(), "abcd1234.0123456789abcdef")
	if err == nil {
		t.Fatal("Enroll() = nil, se esperaba error del servicio")
	}
	if !strings.Contains(err.Error(), "ya está dado de alta") {
		t.Errorf("Enroll() error = %q, no propaga el mensaje del servicio", err)
	}
}

func TestResetHappyPath(t *testing.T) {
	called := false
	client := newTestServerWithReset(t,
		func() Status { return Status{State: StateConnected} },
		func(string) error { return nil },
		func() error { called = true; return nil },
	)

	if err := client.Reset(context.Background()); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if !called {
		t.Error("el reset no se ejecutó")
	}
}

// El error del servicio (p. ej. "ya está sin configurar") debe llegar al
// tray con su texto, igual que TestEnrollPropagatesServiceError.
func TestResetPropagatesServiceError(t *testing.T) {
	client := newTestServerWithReset(t,
		func() Status { return Status{State: StateUnconfigured} },
		func(string) error { return nil },
		func() error { return errors.New("el agente ya está sin configurar") },
	)

	err := client.Reset(context.Background())
	if err == nil {
		t.Fatal("Reset() = nil, se esperaba error del servicio")
	}
	if !strings.Contains(err.Error(), "ya está sin configurar") {
		t.Errorf("Reset() error = %q, no propaga el mensaje del servicio", err)
	}
}

// GET /debug (plan §12.2, Tier 3): goroutines/memoria vienen del propio
// proceso del test (runtime.NumGoroutine siempre > 0), y RecentLog viene de
// la función inyectada.
func TestDebugRoundTrip(t *testing.T) {
	client := newTestServerWithLog(t,
		func() Status { return Status{State: StateConnected} },
		func(string) error { return nil },
		func() error { return nil },
		func() []string { return []string{"línea vieja", "línea reciente"} },
	)

	got, err := client.Debug(context.Background())
	if err != nil {
		t.Fatalf("Debug() error = %v", err)
	}
	if got.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, se esperaba > 0", got.Goroutines)
	}
	want := []string{"línea vieja", "línea reciente"}
	if !reflect.DeepEqual(got.RecentLog, want) {
		t.Errorf("RecentLog = %v, se esperaba %v", got.RecentLog, want)
	}
}

// Sin RecentLogFunc (nil), /debug no debe fallar — solo no trae RecentLog.
func TestDebugWithoutRecentLogFunc(t *testing.T) {
	client := newTestServer(t,
		func() Status { return Status{State: StateConnected} },
		func(string) error { return nil },
	)

	got, err := client.Debug(context.Background())
	if err != nil {
		t.Fatalf("Debug() error = %v", err)
	}
	if len(got.RecentLog) != 0 {
		t.Errorf("RecentLog = %v, se esperaba vacío sin RecentLogFunc", got.RecentLog)
	}
}
