package shipper

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// Sin ajustes no se construye transporte propio: el de Go ya respeta
// HTTP_PROXY/HTTPS_PROXY/NO_PROXY y el almacén del sistema.
func TestNoOptionsLeavesGoDefaultTransport(t *testing.T) {
	client, err := newHTTPClient(Options{})
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}
	if client.Transport != nil {
		t.Error("se construyó un transporte propio sin necesidad; eso pierde el manejo de proxy por entorno")
	}
}

func TestProxyURLIsApplied(t *testing.T) {
	client, err := newHTTPClient(Options{ProxyURL: "http://proxy.empresa.local:3128"})
	if err != nil {
		t.Fatalf("newHTTPClient() error = %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, se esperaba *http.Transport", client.Transport)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://ellysia.example/hygeia", nil)
	proxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxy == nil || proxy.Host != "proxy.empresa.local:3128" {
		t.Errorf("el proxy resuelto es %v, se esperaba proxy.empresa.local:3128", proxy)
	}
}

// url.Parse acepta casi cualquier cosa. "proxy.empresa.local:3128" sin
// esquema se analiza SIN error —esquema "proxy.empresa.local", opaco
// "3128"— y luego el proxy no se usa, en silencio. Es el error de escritura
// más probable de todos y el que peor se diagnostica.
func TestProxyURLWithoutSchemeIsRejected(t *testing.T) {
	_, err := newHTTPClient(Options{ProxyURL: "proxy.empresa.local:3128"})

	if err == nil {
		t.Fatal("newHTTPClient() con un proxy sin esquema = nil, se esperaba error")
	}
	if !strings.Contains(err.Error(), "http://") {
		t.Errorf("el error no enseña la forma correcta: %v", err)
	}
}

func TestBadCAFileIsRejected(t *testing.T) {
	dir := t.TempDir()

	noEsPEM := filepath.Join(dir, "no-es-pem.crt")
	if err := os.WriteFile(noEsPEM, []byte("esto no es un certificado\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	casos := map[string]string{
		"un fichero que no existe": filepath.Join(dir, "no-existe.pem"),
		// AppendCertsFromPEM no devuelve error, devuelve false. Sin
		// comprobarlo, esto se aceptaría en silencio y el fallo aparecería
		// después como un error de certificado sin relación aparente.
		"un fichero sin certificados": noEsPEM,
	}
	for nombre, ruta := range casos {
		if _, err := newHTTPClient(Options{CAFile: ruta}); err == nil {
			t.Errorf("%s: se aceptó sin error", nombre)
		}
	}
}

// La prueba que de verdad importa: un servidor TLS con un certificado que no
// firma ninguna autoridad conocida debe ser rechazado sin caFile y aceptado
// con él. Es el escenario de una red con inspección TLS.
func TestCustomCAMakesAnOtherwiseUntrustedServerReachable(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"nextIntervalSec":15}`))
	}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "empresa-ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	p := &payload.Payload{AgentVersion: "test"}

	// Sin la CA: el certificado no lo firma nadie en quien confiemos.
	// fastBackoff porque un fallo de TLS es transitorio para el shipper y
	// reintenta con espera exponencial: sin esto, esta prueba sola tarda
	// quince segundos.
	sinCA := mustShipper(t, srv.URL, "abcd1234.0123456789abcdef")
	fastBackoff(sinCA)
	if _, err := sinCA.Send(context.Background(), p); err == nil {
		t.Error("el servidor con certificado desconocido se aceptó sin caFile")
	}

	// Con la CA: conecta.
	conCA, err := NewShipper(srv.URL, "abcd1234.0123456789abcdef", Options{CAFile: caPath})
	if err != nil {
		t.Fatalf("NewShipper() con caFile = %v", err)
	}
	if _, err := conCA.Send(context.Background(), p); err != nil {
		t.Errorf("Send() con caFile = %v, se esperaba que conectara", err)
	}
}

// Y lo que no debe perderse por el camino: el almacén del sistema. Poner
// RootCAs con SOLO el certificado corporativo funcionaría dentro de la
// oficina y fallaría en cuanto el equipo saliera de ella.
func TestCustomCAIsAddedToTheSystemStoreAndDoesNotReplaceIt(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "empresa-ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := caPool(caPath)
	if err != nil {
		t.Fatalf("caPool() error = %v", err)
	}

	soloElCorporativo := x509.NewCertPool()
	if !soloElCorporativo.AppendCertsFromPEM(certPEM) {
		t.Fatal("no se pudo construir el almacén de comparación")
	}
	if got.Equal(soloElCorporativo) {
		t.Error("el almacén resultante SUSTITUYE al del sistema en vez de sumarse a él")
	}

	// Y la otra mitad: que efectivamente se haya añadido.
	sistema, err := x509.SystemCertPool()
	if err != nil {
		t.Skipf("no hay almacén del sistema en esta máquina: %v", err)
	}
	if got.Equal(sistema) {
		t.Error("el certificado de caFile no se añadió al almacén")
	}
}
