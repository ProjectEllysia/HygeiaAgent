package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/config"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/shipper"
)

// El subcomando `doctor` (F-04).
//
// `status` dice si el proceso está vivo e `info` si está reportando. Ninguno
// responde a la pregunta que de verdad se hace quien depura: POR QUÉ no
// llega. Cada comprobación de aquí es una hipótesis concreta de por qué un
// agente se queda mudo, en el orden en que se descartan.
//
// Deliberadamente NO usa el canal de control para lo esencial: si el servicio
// no arranca, el canal tampoco está, y ese es justo uno de los casos que hay
// que poder diagnosticar. Doctor lee la configuración y prueba la red por su
// cuenta; solo consulta al servicio al final, para lo que únicamente él sabe.

type level int

const (
	levelOK level = iota
	levelWarn
	levelFail
)

func (l level) label() string {
	switch l {
	case levelOK:
		return "[ok]   "
	case levelWarn:
		return "[aviso]"
	default:
		return "[fallo]"
	}
}

// check es el resultado de una comprobación. hint solo se rellena cuando hay
// algo que hacer: un consejo debajo de cada línea correcta sería ruido.
type check struct {
	name   string
	detail string
	hint   string
	level  level
}

func ok(name, detail string) check {
	return check{name: name, detail: detail, level: levelOK}
}

func warn(name, detail, hint string) check {
	return check{name: name, detail: detail, hint: hint, level: levelWarn}
}

func fail(name, detail, hint string) check {
	return check{name: name, detail: detail, hint: hint, level: levelFail}
}

// doctorTimeout acota cada prueba de red por separado. Corto a propósito:
// quien ejecuta esto está esperando delante de la pantalla, y una
// comprobación que tarda treinta segundos en decir "no llego" es peor que una
// que lo dice en cinco.
const doctorTimeout = 5 * time.Second

// runDoctor ejecuta las comprobaciones en orden y devuelve error si alguna
// falla, para que se pueda usar desde un script.
func runDoctor() error {
	cfg, cfgCheck := loadConfigForDoctor()
	checks := []check{cfgCheck}

	if cfg != nil {
		checks = append(checks,
			checkConfigPermissions(cfg),
			checkServerURL(cfg),
			checkAgentKey(cfg),
			checkNetworkOptions(cfg),
		)
		checks = append(checks, checkConnectivity(cfg)...)
	}
	checks = append(checks, checkService())

	return report(checks)
}

func report(checks []check) error {
	fmt.Println()
	failed := 0
	for _, c := range checks {
		fmt.Printf("  %s %-26s %s\n", c.level.label(), c.name, c.detail)
		if c.hint != "" {
			for _, line := range strings.Split(c.hint, "\n") {
				fmt.Printf("          -> %s\n", line)
			}
		}
		if c.level == levelFail {
			failed++
		}
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("doctor: %d comprobación(es) fallaron", failed)
	}
	return nil
}

// -------------------------------------------------------------------------
// Configuración
// -------------------------------------------------------------------------

func loadConfigForDoctor() (*config.Config, check) {
	path := config.DefaultPath()

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fail("Configuración", path,
			"No se puede leer el fichero.\n"+
				err.Error()+"\n"+
				"Si es un problema de permisos, ejecuta doctor como administrador o root.")
	}
	// config.Load NO falla si el fichero no existe: devuelve los valores por
	// defecto. Para doctor esa distinción es justo lo que interesa.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, warn("Configuración", path+" (no existe)",
			"El agente está usando los valores por defecto y no tiene servidor ni clave.\n"+
				"Se crea al dar de alta el agente: hygeia-agent enroll")
	}
	return cfg, ok("Configuración", path)
}

func checkConfigPermissions(cfg *config.Config) check {
	const name = "Permisos de la config"
	path := cfg.Path()

	info, err := os.Stat(path)
	if err != nil {
		return warn(name, "no se pudo comprobar", err.Error())
	}

	// En Windows los permisos son una DACL y no un modo octal; leerla e
	// interpretarla aquí sería bastante código para una comprobación
	// secundaria. El agente ya fija la DACL correcta al guardar la clave
	// (config.Save), así que se informa en vez de fingir que se comprueba.
	if runtime.GOOS == "windows" {
		return ok(name, "los fija el servicio al guardar la clave (DACL)")
	}

	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fail(name, fmt.Sprintf("%04o — lo puede leer otro usuario", mode),
			"El fichero contiene la clave del agente.\n"+
				"Corrígelo con: chmod 600 "+path)
	}
	return ok(name, fmt.Sprintf("%04o", info.Mode().Perm()))
}

func checkServerURL(cfg *config.Config) check {
	const name = "URL del servidor"

	if cfg.ServerURL == "" {
		return fail(name, "sin definir",
			"Ponla en "+cfg.Path()+" (campo serverUrl).")
	}
	u, err := url.Parse(cfg.ServerURL)
	if err != nil || u.Host == "" {
		return fail(name, cfg.ServerURL, "No es una URL válida.")
	}
	if u.Scheme != "https" {
		return warn(name, cfg.ServerURL,
			"No es https: la clave de agente viaja en claro en cada envío.")
	}
	return ok(name, cfg.ServerURL)
}

func checkAgentKey(cfg *config.Config) check {
	const name = "Clave de agente"

	if cfg.AgentKey == "" {
		return fail(name, "sin definir",
			"El agente no reporta nada sin clave. Pide una en Ellysia y aplícala:\n"+
				`echo "$CLAVE" | hygeia-agent enroll`)
	}
	if err := control.ValidateAgentKey(cfg.AgentKey); err != nil {
		return fail(name, err.Error(),
			"Pégala tal cual la emite Ellysia, sin recortar ni añadir espacios.")
	}
	// El secreto NO se imprime. Doctor es lo primero que alguien pega en un
	// ticket de soporte o en un chat.
	keyID, _, _ := strings.Cut(cfg.AgentKey, ".")
	return ok(name, "formato válido (keyId "+keyID+")")
}

func checkNetworkOptions(cfg *config.Config) check {
	const name = "Ajustes de red"

	switch {
	case cfg.ProxyURL == "" && cfg.CAFile == "":
		return ok(name, "por defecto (respeta HTTP_PROXY y el almacén del sistema)")
	default:
		// Se validan construyendo el emisor de verdad, que es exactamente lo
		// que hará el servicio al arrancar: así doctor no puede decir que
		// están bien si el agente no va a poder usarlos.
		if _, err := shipper.NewShipper(cfg.ServerURL, cfg.AgentKey,
			shipper.Options{ProxyURL: cfg.ProxyURL, CAFile: cfg.CAFile}); err != nil {
			return fail(name, "inválidos", err.Error())
		}
		detail := []string{}
		if cfg.ProxyURL != "" {
			detail = append(detail, "proxy "+redactProxyCredentials(cfg.ProxyURL))
		}
		if cfg.CAFile != "" {
			detail = append(detail, "CA propia "+cfg.CAFile)
		}
		return ok(name, strings.Join(detail, ", "))
	}
}

// redactProxyCredentials quita usuario y contraseña de la URL del proxy: se
// admiten ahí (F-09) y esta salida acaba pegada en tickets de soporte.
func redactProxyCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	// "oculto" y no "***": url.String() codifica la parte de usuario, y los
	// asteriscos salían como "%2A%2A%2A". La contraseña quedaba oculta igual,
	// pero la línea era ilegible.
	u.User = url.User("oculto")
	return u.String()
}

// -------------------------------------------------------------------------
// Red
// -------------------------------------------------------------------------

func checkConnectivity(cfg *config.Config) []check {
	u, err := url.Parse(cfg.ServerURL)
	if err != nil || u.Host == "" {
		return nil // ya lo dijo checkServerURL
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}

	dns := checkDNS(host)
	checks := []check{dns}
	if dns.level == levelFail {
		// Sin resolver el nombre, TCP y TLS fallarían por lo mismo y
		// repetirlo tres veces solo esconde cuál es el problema de verdad.
		return checks
	}

	tcp := checkTCP(host, port)
	checks = append(checks, tcp)
	if tcp.level == levelFail {
		return checks
	}

	if u.Scheme == "https" {
		checks = append(checks, checkTLS(cfg, host, port))
	}
	checks = append(checks, checkAuthAndClock(cfg)...)
	return checks
}

func checkDNS(host string) check {
	const name = "Resolución DNS"

	// Una IP literal no se resuelve, y decir "fallo" ahí sería mentira.
	if net.ParseIP(host) != nil {
		return ok(name, host+" (es una IP, no hace falta resolver)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return fail(name, host,
			"No se resuelve el nombre.\n"+
				"Comprueba el DNS del equipo, o si el nombre solo existe en la red interna.")
	}
	return ok(name, host+" -> "+strings.Join(addrs, ", "))
}

func checkTCP(host, port string) check {
	const name = "Conectividad TCP"
	target := net.JoinHostPort(host, port)

	inicio := time.Now()
	conn, err := net.DialTimeout("tcp", target, doctorTimeout)
	if err != nil {
		return fail(name, target,
			"No se puede abrir la conexión.\n"+
				"Suele ser un cortafuegos, o un proxy obligatorio sin configurar (campo proxyUrl).")
	}
	_ = conn.Close()

	return ok(name, fmt.Sprintf("%s alcanzable (%d ms)", target, time.Since(inicio).Milliseconds()))
}

func checkTLS(cfg *config.Config, host, port string) check {
	const name = "Certificado TLS"
	target := net.JoinHostPort(host, port)

	// Con la MISMA configuración de CA que usará el agente: sin esto, doctor
	// diría que el certificado es válido usando el almacén del sistema
	// mientras el servicio falla por no encontrar la CA corporativa, que es
	// precisamente el caso que F-09 resuelve.
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pool, err := caPoolForDoctor(cfg.CAFile)
		if err != nil {
			return fail(name, "no se pudo preparar la CA propia", err.Error())
		}
		tlsCfg.RootCAs = pool
	}

	dialer := &net.Dialer{Timeout: doctorTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", target, tlsCfg)
	if err != nil {
		hint := "El certificado del servidor no se acepta.\n" + err.Error()
		if cfg.CAFile == "" {
			hint += "\nEn una red con inspección TLS hace falta añadir la CA de la empresa (campo caFile)."
		}
		return fail(name, target, hint)
	}
	defer func() { _ = conn.Close() }()

	cert := conn.ConnectionState().PeerCertificates[0]
	quedan := time.Until(cert.NotAfter)
	detalle := fmt.Sprintf("válido, caduca el %s", cert.NotAfter.Format("2006-01-02"))

	// Avisar antes de que caduque es la diferencia entre un mantenimiento y
	// una flota entera muda de golpe.
	if quedan < 15*24*time.Hour {
		return warn(name, detalle,
			fmt.Sprintf("Caduca en %d días. Cuando lo haga, ningún agente podrá enviar nada.",
				int(quedan.Hours()/24)))
	}
	return ok(name, detalle)
}

func caPoolForDoctor(path string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%q no contiene ningún certificado PEM válido", path)
	}
	return pool, nil
}

// checkAuthAndClock hace UNA petición y saca de ella dos respuestas.
//
// El cuerpo que se envía es un objeto vacío, a propósito: sirve para saber si
// la clave se acepta sin dar de alta un heartbeat falso en el activo. Si el
// backend responde 422 (esquema inválido) es que la autenticación pasó, que
// es justo lo que se quería averiguar; si responde 401, no.
//
// Y la cabecera Date de esa misma respuesta da la hora del servidor sin
// necesitar un envío correcto, que es lo que permite detectar el reloj
// desincronizado — hoy ese fallo se manifiesta como un 400 opaco.
func checkAuthAndClock(cfg *config.Config) []check {
	const authName = "Autenticación"
	const clockName = "Desviación de reloj"

	// El MISMO cliente que usará el servicio, proxy y CA incluidos. Montar
	// uno propio aquí permitiría que doctor diera por buena una red que el
	// agente no puede usar, que es justo el fallo que viene a evitar.
	client, err := shipper.NewHTTPClient(shipper.Options{ProxyURL: cfg.ProxyURL, CAFile: cfg.CAFile})
	if err != nil {
		return []check{fail(authName, "no se pudo preparar la petición", err.Error())}
	}
	client.Timeout = doctorTimeout

	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(cfg.ServerURL, "/")+"/ingest", strings.NewReader("{}"))
	if err != nil {
		return []check{fail(authName, "no se pudo construir la petición", err.Error())}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.AgentKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hygeia-agent doctor")

	resp, err := client.Do(req)
	if err != nil {
		return []check{fail(authName, "sin respuesta", err.Error())}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	return []check{
		authCheck(authName, resp.StatusCode),
		clockCheck(clockName, resp.Header.Get("Date"), time.Now()),
	}
}

func authCheck(name string, status int) check {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fail(name, fmt.Sprintf("el servidor respondió %d", status),
			"La clave ha sido revocada o rotada. Pide una nueva en Ellysia y aplícala:\n"+
				`echo "$CLAVE" | hygeia-agent enroll`)
	case status == http.StatusProxyAuthRequired:
		return fail(name, "el proxy pidió credenciales (407)",
			"No es la clave de agente: es el proxy.\n"+
				"Ponlas en la propia URL: proxyUrl = \"http://usuario:clave@proxy:3128\"")
	case status >= 500:
		return fail(name, fmt.Sprintf("el servidor respondió %d", status),
			"El problema está en el servidor, no en este agente.")
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		// Lo esperado: el cuerpo vacío se rechaza DESPUÉS de comprobar la
		// clave, así que llegar hasta aquí significa que la clave vale.
		return ok(name, "la clave es válida")
	case status == http.StatusTooManyRequests:
		return ok(name, "la clave es válida (el envío llegó antes del suelo de cadencia)")
	default:
		return ok(name, fmt.Sprintf("el servidor respondió %d", status))
	}
}

// maxClockSkew es la ventana hacia el futuro del backend
// (features.hygeia.limits.clockSkewSec). Pasarse cuesta el heartbeat entero.
const maxClockSkew = 300 * time.Second

func clockCheck(name, dateHeader string, now time.Time) check {
	if dateHeader == "" {
		return warn(name, "el servidor no envió cabecera Date", "No se pudo comprobar.")
	}
	serverTime, err := http.ParseTime(dateHeader)
	if err != nil {
		return warn(name, "cabecera Date ilegible: "+dateHeader, "No se pudo comprobar.")
	}

	skew := now.Sub(serverTime)
	abs := skew
	if abs < 0 {
		abs = -abs
	}
	// La cabecera Date tiene resolución de un segundo y la petición tarda lo
	// suyo, así que unos pocos segundos de diferencia no significan nada.
	if abs <= 5*time.Second {
		return ok(name, "el reloj local coincide con el del servidor")
	}

	sentido := "por delante"
	if skew < 0 {
		sentido = "por detrás"
	}
	detalle := fmt.Sprintf("el reloj local va %d s %s", int(abs.Seconds()), sentido)

	if abs >= maxClockSkew {
		return fail(name, detalle,
			fmt.Sprintf("El servidor rechaza heartbeats con más de %d s de desviación hacia el futuro.\n",
				int(maxClockSkew.Seconds()))+
				"Sincroniza el reloj del sistema (NTP).")
	}
	return warn(name, detalle, "Todavía dentro de la ventana que acepta el servidor, pero conviene sincronizarlo.")
}

// -------------------------------------------------------------------------
// Servicio
// -------------------------------------------------------------------------

// checkService es lo único que pregunta al canal de control, y va al final a
// propósito: todo lo anterior se puede diagnosticar con el servicio parado, y
// que esté parado es precisamente uno de los diagnósticos posibles.
func checkService() check {
	const name = "Servicio y buffer"

	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	st, err := control.NewClient().Status(ctx)
	if err != nil {
		return warn(name, "no responde",
			"El servicio no está en marcha, o no se puede hablar con él.\n"+
				"Arráncalo con: hygeia-agent start")
	}

	detalle := fmt.Sprintf("estado %q, %d payloads en el buffer", st.State, st.BufferSize)
	switch st.State {
	case control.StateKeyRejected:
		return fail(name, detalle,
			"El servicio ya ha marcado la clave como rechazada.\n"+
				`Aplica la nueva con: echo "$CLAVE" | hygeia-agent enroll`)
	case control.StateLocalError:
		return warn(name, detalle, "Último error: "+st.LastError)
	default:
		return ok(name, detalle)
	}
}
