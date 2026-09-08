//go:build linux || windows

package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fakeProvider(lookPathOK bool, run func(ctx context.Context, bin string) ([]byte, error)) *nvidiaGPUProvider {
	lookPath := func(string) (string, error) {
		if lookPathOK {
			return "/usr/bin/nvidia-smi", nil
		}
		return "", errors.New("no encontrado")
	}
	return &nvidiaGPUProvider{log: discardLogger(), lookPath: lookPath, run: run}
}

func TestNVIDIAProviderSingleGPU(t *testing.T) {
	p := fakeProvider(true, func(ctx context.Context, bin string) ([]byte, error) {
		return []byte("150.25\n"), nil
	})
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil {
		t.Fatal("Read() = nil, se esperaba la potencia de una GPU")
	}
	if pw.Watts != 150.25 {
		t.Errorf("Watts = %v, want 150.25", pw.Watts)
	}
	if pw.Estimated {
		t.Error("Estimated = true, nvidia-smi es telemetría del propio dispositivo")
	}
	if pw.Source != "nvidia" {
		t.Errorf("Source = %q, want %q", pw.Source, "nvidia")
	}
}

func TestNVIDIAProviderSumsMultipleGPUs(t *testing.T) {
	p := fakeProvider(true, func(ctx context.Context, bin string) ([]byte, error) {
		return []byte("100.0\n120.0\n"), nil
	})
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw == nil || pw.Watts != 220.0 {
		t.Errorf("Read() = %+v, se esperaba Watts=220 (100+120)", pw)
	}
}

func TestNVIDIAProviderAbsentDoesNotRepeatLookup(t *testing.T) {
	calls := 0
	lookPath := func(string) (string, error) {
		calls++
		return "", errors.New("no encontrado")
	}
	p := &nvidiaGPUProvider{
		log:      discardLogger(),
		lookPath: lookPath,
		run: func(ctx context.Context, bin string) ([]byte, error) {
			t.Fatal("run no debería llamarse si nvidia-smi no está instalado")
			return nil, nil
		},
	}

	for range 3 {
		pw, err := p.Read(context.Background())
		if err != nil || pw != nil {
			t.Fatalf("Read() = (%+v, %v), se esperaba (nil, nil)", pw, err)
		}
	}
	if calls != 1 {
		t.Errorf("lookPath se llamó %d veces, se esperaba 1 (cacheado tras el primer ciclo)", calls)
	}
}

func TestNVIDIAProviderUnreadableOutputReturnsNil(t *testing.T) {
	p := fakeProvider(true, func(ctx context.Context, bin string) ([]byte, error) {
		return []byte("no soy un número\n"), nil
	})
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil: salida ilegible", pw)
	}
}

func TestNVIDIAProviderCommandErrorReturnsNilWithoutError(t *testing.T) {
	p := fakeProvider(true, func(ctx context.Context, bin string) ([]byte, error) {
		return nil, errors.New("nvidia-smi: driver no responde")
	})
	pw, err := p.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() err = %v, un fallo de nvidia-smi no debe propagarse", err)
	}
	if pw != nil {
		t.Errorf("Read() = %+v, se esperaba nil", pw)
	}
}

// TestNVIDIAProviderRespectsContextTimeout comprueba que Read no se queda
// colgado si nvidia-smi no responde: el run simulado bloquea hasta que su
// propio ctx (acotado por nvidiaSMITimeout) se cancela, y entonces Read debe
// devolver nil sin error en vez de esperar indefinidamente (P27: cancelación
// de contexto a mitad de una lectura).
func TestNVIDIAProviderRespectsContextTimeout(t *testing.T) {
	p := fakeProvider(true, func(ctx context.Context, bin string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if v, err := p.Read(context.Background()); err != nil || v != nil {
			t.Errorf("Read() = (%+v, %v), se esperaba (nil, nil)", v, err)
		}
	}()

	select {
	case <-done:
	case <-time.After(nvidiaSMITimeout + 2*time.Second):
		t.Fatal("Read() no volvió tras agotarse nvidiaSMITimeout: se quedó colgado")
	}
}
