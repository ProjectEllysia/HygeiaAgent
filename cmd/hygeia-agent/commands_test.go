package main

import (
	"strings"
	"testing"
)

const validKey = "abcd1234.0123456789abcdef"

// La clave puede venir como argumento o por la entrada estándar. Las dos
// formas existen porque sirven para cosas distintas: el argumento es cómodo a
// mano, la entrada estándar es la que no deja la clave a la vista en `ps`.
func TestAgentKeyFromTheArgument(t *testing.T) {
	got, err := agentKey([]string{validKey}, strings.NewReader(""))

	if err != nil {
		t.Fatalf("agentKey() error = %v", err)
	}
	if got != validKey {
		t.Errorf("agentKey() = %q, se esperaba %q", got, validKey)
	}
}

func TestAgentKeyFromStandardInput(t *testing.T) {
	entradas := map[string]string{
		"con salto de línea":     validKey + "\n",
		"sin salto final":        validKey,
		"con espacios sobrantes": "  " + validKey + "  \n",
		// `echo` en Windows y los ficheros con finales CRLF.
		"con retorno de carro": validKey + "\r\n",
	}
	for nombre, entrada := range entradas {
		got, err := agentKey(nil, strings.NewReader(entrada))
		if err != nil {
			t.Errorf("%s: agentKey() error = %v", nombre, err)
			continue
		}
		if got != validKey {
			t.Errorf("%s: agentKey() = %q, se esperaba %q", nombre, got, validKey)
		}
	}
}

// Un argumento vacío (`hygeia-agent enroll ""`) no debe tomarse por una clave:
// hay que caer a la entrada estándar, no mandar la cadena vacía al servicio.
func TestAgentKeyFallsBackWhenTheArgumentIsBlank(t *testing.T) {
	got, err := agentKey([]string{"   "}, strings.NewReader(validKey+"\n"))

	if err != nil {
		t.Fatalf("agentKey() error = %v", err)
	}
	if got != validKey {
		t.Errorf("agentKey() = %q, se esperaba la clave de la entrada estándar", got)
	}
}

// Sin clave por ningún lado, el error tiene que decir cómo se usa. Es
// exactamente el momento en que alguien está dando de alta su primer agente
// por SSH y no tiene el manual delante.
func TestAgentKeyWithoutAnyKeyExplainsHowToPassIt(t *testing.T) {
	_, err := agentKey(nil, strings.NewReader("\n"))

	if err == nil {
		t.Fatal("agentKey() sin clave = nil, se esperaba error")
	}
	for _, want := range []string{"hygeia-agent enroll", "echo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el error no menciona %q: %v", want, err)
		}
	}
}

// `reset` borra la clave y deja el activo mudo. Se parece lo bastante a
// `restart` como para teclear uno por otro, así que la confirmación solo
// acepta un sí explícito.
func TestConfirmedOnlyAcceptsAnExplicitYes(t *testing.T) {
	acepta := []string{"s\n", "S\n", "si\n", "sí\n", "y\n", "Y\n", "yes\n", "  s  \n"}
	for _, in := range acepta {
		if !confirmed(strings.NewReader(in)) {
			t.Errorf("confirmed(%q) = false, se esperaba true", in)
		}
	}

	rechaza := []string{
		"\n",      // solo Intro: el caso por defecto, y el más importante
		"n\n",     // no
		"no\n",    //
		"",        // entrada cerrada sin escribir nada
		"sip\n",   // parecido pero no es
		"quizá\n", //
	}
	for _, in := range rechaza {
		if confirmed(strings.NewReader(in)) {
			t.Errorf("confirmed(%q) = true, se esperaba false", in)
		}
	}
}

// El texto de ayuda es lo único que ve alguien que no conoce el mandato.
func TestUsageListsTheNewSubcommands(t *testing.T) {
	for _, cmd := range []string{"enroll", "reset", "info", "debug"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("la ayuda no menciona el subcomando %q", cmd)
		}
	}
	// Y avisa de por qué no conviene pasar la clave como argumento.
	if !strings.Contains(usage, "ps") {
		t.Error("la ayuda no advierte de que un argumento queda visible en `ps`")
	}
}
