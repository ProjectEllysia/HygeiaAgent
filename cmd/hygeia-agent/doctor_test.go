package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/config"
)

// Cada código tiene que llevar a un consejo DISTINTO. Es el punto entero de
// doctor: 401 y 407 se parecen y mandan a sitios opuestos —rotar la clave o
// configurar el proxy—, y confundirlos hace perder la tarde.
func TestAuthCheckSeparatesTheKeyFromTheProxy(t *testing.T) {
	casos := []struct {
		status      int
		nivel       level
		enElConsejo string
	}{
		{http.StatusUnauthorized, levelFail, "revocada"},
		{http.StatusForbidden, levelFail, "revocada"},
		{http.StatusProxyAuthRequired, levelFail, "proxy"},
		{http.StatusInternalServerError, levelFail, "servidor"},
	}
	for _, c := range casos {
		got := authCheck("Autenticación", c.status)
		if got.level != c.nivel {
			t.Errorf("status %d: nivel = %v, se esperaba %v", c.status, got.level, c.nivel)
		}
		if !strings.Contains(strings.ToLower(got.hint), c.enElConsejo) {
			t.Errorf("status %d: el consejo no menciona %q: %s", c.status, c.enElConsejo, got.hint)
		}
	}
}

// Un cuerpo vacío rechazado por esquema significa que la clave SÍ pasó: el
// backend valida la credencial antes que el cuerpo. Es lo que permite
// comprobar la autenticación sin dar de alta un heartbeat falso.
func TestAuthCheckTreatsSchemaRejectionAsAValidKey(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		if got := authCheck("Autenticación", status); got.level != levelOK {
			t.Errorf("status %d: nivel = %v, se esperaba ok — llegar al esquema significa que la clave vale",
				status, got.level)
		}
	}
}

func TestClockCheck(t *testing.T) {
	ahora := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	comoCabecera := func(t time.Time) string { return t.Format(http.TimeFormat) }

	casos := map[string]struct {
		date  string
		nivel level
	}{
		"en hora":                    {comoCabecera(ahora), levelOK},
		"dos segundos de diferencia": {comoCabecera(ahora.Add(-2 * time.Second)), levelOK},
		// Aún dentro de la ventana del backend, pero merece un aviso.
		"un minuto por delante": {comoCabecera(ahora.Add(-1 * time.Minute)), levelWarn},
		// Pasada la ventana: el backend rechaza el heartbeat entero, y hoy
		// eso se manifiesta como un 400 opaco.
		"siete minutos por delante": {comoCabecera(ahora.Add(-7 * time.Minute)), levelFail},
		"siete minutos por detrás":  {comoCabecera(ahora.Add(7 * time.Minute)), levelFail},
		"sin cabecera":              {"", levelWarn},
		"cabecera ilegible":         {"no es una fecha", levelWarn},
	}
	for nombre, c := range casos {
		got := clockCheck("Reloj", c.date, ahora)
		if got.level != c.nivel {
			t.Errorf("%s: nivel = %v, se esperaba %v (detalle: %s)", nombre, got.level, c.nivel, got.detail)
		}
	}
}

// La salida de doctor es lo primero que alguien pega en un ticket de soporte.
func TestDoctorNeverPrintsTheSecret(t *testing.T) {
	const secreto = "0123456789abcdefSECRETO"
	cfg := &config.Config{AgentKey: "abcd1234." + secreto}

	got := checkAgentKey(cfg)

	if strings.Contains(got.detail+got.hint, secreto) {
		t.Errorf("el secreto de la clave aparece en la salida: %s %s", got.detail, got.hint)
	}
	if !strings.Contains(got.detail, "abcd1234") {
		t.Errorf("no se enseña el keyId, que sí es útil y no es secreto: %s", got.detail)
	}
}

func TestRedactProxyCredentials(t *testing.T) {
	casos := map[string]string{
		"http://usuario:clave@proxy.empresa.local:3128": "oculto@proxy.empresa.local:3128",
		"http://proxy.empresa.local:3128":               "proxy.empresa.local:3128",
	}
	for entrada, esperado := range casos {
		got := redactProxyCredentials(entrada)
		if !strings.Contains(got, esperado) {
			t.Errorf("redactProxyCredentials(%q) = %q, se esperaba que contuviera %q", entrada, got, esperado)
		}
		if strings.Contains(got, "clave") {
			t.Errorf("redactProxyCredentials(%q) = %q: la contraseña sigue ahí", entrada, got)
		}
	}
}

func TestCheckServerURL(t *testing.T) {
	casos := map[string]struct {
		url   string
		nivel level
	}{
		"correcta":      {"https://ellysia.example/hygeia", levelOK},
		"sin definir":   {"", levelFail},
		"no es una URL": {"://roto", levelFail},
		// http funciona, pero la clave viaja en claro en cada envío.
		"sin cifrar": {"http://ellysia.example/hygeia", levelWarn},
	}
	for nombre, c := range casos {
		got := checkServerURL(&config.Config{ServerURL: c.url})
		if got.level != c.nivel {
			t.Errorf("%s: nivel = %v, se esperaba %v", nombre, got.level, c.nivel)
		}
	}
}

// El código de salida tiene que distinguir "todo bien" de "hay algo roto",
// para poder usar doctor desde un script de despliegue.
func TestReportFailsOnlyWhenSomethingFailed(t *testing.T) {
	sinFallos := []check{ok("a", ""), warn("b", "", "consejo")}
	if err := report(sinFallos); err != nil {
		t.Errorf("report() con solo avisos = %v, se esperaba nil", err)
	}

	conFallo := []check{ok("a", ""), fail("b", "", "consejo")}
	if err := report(conFallo); err == nil {
		t.Error("report() con un fallo = nil, se esperaba error")
	}
}
