package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
)

// Las pruebas de arriba cubren de dónde sale la clave. Estas cubren lo otro:
// que los subcomandos hablen de verdad con el canal de control y le lleguen
// los datos correctos. Sin esto, `enroll` podría analizar la clave
// perfectamente y no entregarla a nadie.
//
// Se levanta un servidor de control real sobre un transporte propio de la
// prueba, para no hablar con el hygeia-agent que pueda estar instalado en la
// máquina donde corren las pruebas.

type fakeService struct {
	enrolled string
	resetted bool
	status   control.Status
}

func startControlChannel(t *testing.T, svc *fakeService) {
	t.Helper()
	isolateControl(t)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := control.NewServer(log,
		func() control.Status { return svc.status },
		func(key string) error { svc.enrolled = key; return nil },
		func() error { svc.resetted = true; return nil },
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
			t.Error("el canal de control no cerró a tiempo")
		}
	})

	// Esperar a que el transporte acepte conexiones.
	client := control.NewClient()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			errCh <- err
			t.Fatalf("el canal de control no pudo arrancar: %v", err)
		default:
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := client.Status(cctx)
		ccancel()
		if err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("el canal de control no llegó a aceptar conexiones")
}

// isolateControl apunta cliente y servidor a un transporte propio.
//
// En Unix la ruta se construye bajo /tmp y no bajo t.TempDir(): la dirección
// de un socket Unix viaja en un campo de tamaño fijo de 104 bytes en macOS, y
// allí os.TempDir() cuelga de /var/folders/… y no cabe. Es el mismo motivo
// que en internal/control/isolate_test.go.
func isolateControl(t *testing.T) {
	t.Helper()
	unique := fmt.Sprintf("hygeia-cli-%d-%d", os.Getpid(), time.Now().UnixNano()%1e6)
	if runtime.GOOS == "windows" {
		t.Setenv("HYGEIA_CONTROL_PIPE", `\\.\pipe\`+unique)
		return
	}
	path := filepath.Join("/tmp", unique+".sock")
	t.Cleanup(func() { _ = os.Remove(path) })
	t.Setenv("HYGEIA_CONTROL_SOCKET", path)
}

func TestEnrollDeliversTheKeyToTheService(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateUnconfigured}}
	startControlChannel(t, svc)

	if err := runEnroll([]string{validKey}); err != nil {
		t.Fatalf("runEnroll() error = %v", err)
	}
	if svc.enrolled != validKey {
		t.Errorf("el servicio recibió %q, se esperaba %q", svc.enrolled, validKey)
	}
}

// La validación de la clave la hace el canal de control, no el subcomando.
// Esta prueba fija que el subcomando NO se la salta.
func TestEnrollRejectsAMalformedKeyBeforeTouchingTheService(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateUnconfigured}}
	startControlChannel(t, svc)

	if err := runEnroll([]string{"no-es-una-clave"}); err == nil {
		t.Fatal("runEnroll() con clave malformada = nil, se esperaba error")
	}
	if svc.enrolled != "" {
		t.Errorf("el servicio recibió %q pese a estar malformada", svc.enrolled)
	}
}

func TestResetReachesTheService(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateConnected}}
	startControlChannel(t, svc)

	// --yes: sin él pediría confirmación por terminal, y en una prueba la
	// entrada estándar no lo es.
	if err := runReset([]string{"--yes"}); err != nil {
		t.Fatalf("runReset() error = %v", err)
	}
	if !svc.resetted {
		t.Error("el servicio no ejecutó el reset")
	}
}

// Sin terminal donde confirmar y sin --yes, reset no debe llegar al servicio.
// Es la protección contra un `reset` tecleado en vez de `restart` dentro de un
// script, o con la entrada redirigida.
func TestResetRefusesToRunUnattendedWithoutAnExplicitYes(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateConnected}}
	startControlChannel(t, svc)

	err := resetWith(nil, false, strings.NewReader(""))

	if err == nil {
		t.Fatal("reset sin --yes y sin terminal = nil, se esperaba error")
	}
	if svc.resetted {
		t.Error("el reset llegó al servicio pese a no estar confirmado")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("el error no dice cómo confirmarlo: %v", err)
	}
}

// Y desde un terminal, un "no" tampoco debe llegar al servicio.
func TestResetStopsWhenTheUserDeclines(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateConnected}}
	startControlChannel(t, svc)

	if err := resetWith(nil, true, strings.NewReader("n\n")); err != nil {
		t.Fatalf("reset rechazado = %v, se esperaba nil (cancelar no es un fallo)", err)
	}
	if svc.resetted {
		t.Error("el reset llegó al servicio pese a haberse contestado que no")
	}
}

// Contestando que sí, sí llega.
func TestResetProceedsWhenTheUserAccepts(t *testing.T) {
	svc := &fakeService{status: control.Status{State: control.StateConnected}}
	startControlChannel(t, svc)

	if err := resetWith(nil, true, strings.NewReader("s\n")); err != nil {
		t.Fatalf("reset aceptado = %v", err)
	}
	if !svc.resetted {
		t.Error("el servicio no ejecutó el reset pese a haberse confirmado")
	}
}

func TestInfoReadsTheAgentState(t *testing.T) {
	svc := &fakeService{status: control.Status{
		State:        control.StateConnected,
		AgentVersion: "9.9.9",
		Hostname:     "web-01",
		BufferSize:   7,
	}}
	startControlChannel(t, svc)

	if err := runInfo(); err != nil {
		t.Fatalf("runInfo() error = %v", err)
	}
}

// Sin servicio en marcha, los tres deben fallar con un mensaje que sugiera la
// causa más probable en vez de un error de red pelado.
func TestControlSubcommandsExplainThatTheServiceIsNotRunning(t *testing.T) {
	isolateControl(t) // transporte propio, pero nadie escuchando

	subcomandos := map[string]func() error{
		"enroll": func() error { return runEnroll([]string{validKey}) },
		"reset":  func() error { return runReset([]string{"--yes"}) },
		"info":   runInfo,
	}
	for nombre, run := range subcomandos {
		err := run()
		if err == nil {
			t.Errorf("%s sin servicio = nil, se esperaba error", nombre)
			continue
		}
		if !strings.Contains(err.Error(), "servicio") {
			t.Errorf("%s: el error no orienta sobre la causa: %v", nombre, err)
		}
	}
}
