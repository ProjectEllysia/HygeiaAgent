# Guía de Go para Hygeia — instalación, programación y compilación

> Guía pensada para alguien que **no ha programado nunca en Go**. Parte de cero
> y llega hasta compilar y cruzar-compilar el agente Hygeia. Si ya sabes Go,
> salta directamente a §7 (implementar los TODO) y §8 (compilación).

El repositorio ya contiene el **esqueleto compilable** del agente. Esta guía te
enseña a instalar Go, a entender cada archivo del esqueleto y a rellenar los
`TODO` para tener el agente real.

---

## Índice

1. [Qué es Go y por qué para Hygeia](#1-qué-es-go-y-por-qué-para-hygeia)
2. [Instalación](#2-instalación)
3. [Hola mundo y `go run`](#3-hola-mundo-y-go-run)
4. [Conceptos esenciales de Go](#4-conceptos-esenciales-de-go)
5. [Módulos y dependencias](#5-módulos-y-dependencias)
6. [Estructura del proyecto Hygeia (cómo leer el esqueleto)](#6-estructura-del-proyecto-hygeia-cómo-leer-el-esqueleto)
7. [Implementar los TODO (gopsutil, TOML, shipper, buffer)](#7-implementar-los-todo-gopsutil-toml-shipper-buffer)
8. [Compilación y cross-compilación](#8-compilación-y-cross-compilación)
9. [Testing y calidad](#9-testing-y-calidad)
10. [Empaquetado como servicio](#10-empaquetado-como-servicio)
11. [Flujo de trabajo diario](#11-flujo-de-trabajo-diario)
12. [Recursos oficiales](#12-recursos-oficiales)

---

## 1. Qué es Go y por qué para Hygeia

Go (o *Golang*) es un lenguaje compilado, con tipado estático, creado por Google
en 2009. Sus rasgos clave para un agente de monitorización como Hygeia:

- **Un solo binario estático.** Compilas y obtienes un `.exe` (Windows) o un
  ejecutable (Linux/macOS) que **no necesita runtime ni dependencias
  instaladas** en el host. Lo copias y arranca. Esto es lo que gana a Python
  para distribución (ver README §2).
- **Cross-compilación nativa.** Desde tu Windows puedes compilar para Linux
  ARM64 cambiando dos variables de entorno. Sin toolchains extra.
- **Goroutines.** Concurrencia barata: lanzar miles de "hilos ligeros" cuesta
  KB de RAM. Ideal para correr varios colectores a la vez (README §4).
- **Stdlib potente.** `net/http`, `encoding/json`, `compress/gzip`,
  `log/slog`, `context`, `os/signal`… el núcleo del agente se hace casi sin
  librerías externas.
- **`gopsutil`** da CPU/memoria/disco/red/procesos multiplataforma de fábrica.

Go **no** usa clases ni herencia. Usa `struct` + `interface` + composición. No
hay `try/catch`: los errores son valores que devuelves y compruebas. Es un
lenguaje pequeño: la especificación cabe en ~50 páginas, y se aprende en días.

---

## 2. Instalación

### 2.1 Windows (este equipo)

**Opción A — instalador oficial (recomendada):**
1. Ve a https://go.dev/dl/
2. Descarga `go1.22.x.windows-amd64.msi` (o la versión más reciente).
3. Ejecútalo. Por defecto instala en `C:\Program Files\Go` y **añade Go al PATH
   automáticamente**.
4. Abre una terminal **nueva** (PowerShell) y verifica:

```powershell
go version
# go version go1.22.x windows/amd64
```

**Opción B — winget:**
```powershell
winget install GoLang.Go
```

**Opción C — scoop:**
```powershell
scoop install go
```

> Si `go` no se reconoce tras instalar, abre una terminal nueva. Si siguen sin
> reconocerse, añade `C:\Program Files\Go\bin` a la variable de entorno `PATH`.

### 2.2 Linux

```bash
# Descarga (ejemplo amd64; cambia amd64 por arm64 si tu host es ARM)
wget https://go.dev/dl/go1.22.x.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.x.linux-amd64.tar.gz

# Añade al PATH (en ~/.bashrc o ~/.zshrc)
export PATH=$PATH:/usr/local/go/bin
```

Distros basadas en Debian/Ubuntu también: `sudo apt install golang-go` (suele
quedar una versión antigua; preferible el tarball oficial).

### 2.3 macOS

```bash
brew install go
```

### 2.4 Variables de entorno importantes

Comprueba con `go env`:

| Variable | Significado | Valor típico |
|---|---|---|
| `GOROOT` | Dónde está instalado Go | `C:\Program Files\Go` |
| `GOPATH` | Tu espacio de trabajo (binarios instalados con `go install`) | `~/go` |
| `GOMODCACHE` | Caché de módulos descargados | `~/go/pkg/mod` |
| `GOOS` / `GOARCH` | Sistema/archivo objetivo (para cross-compile) | `windows`/`amd64` |

> **No necesitas** tocar `GOPATH` ni crear `$GOPATH/src/...`. Ese flujo es de
> 2014. Hoy se trabaja con **módulos** (§5) en cualquier carpeta.

### 2.5 Editor

- **VS Code** + la extensión oficial **"Go"** (de Google). Instala `gopls`
  (servidor de lenguaje) automáticamente. Es lo recomendado y gratis.
- **GoLand** (JetBrains, de pago): el IDE más potente para Go.
- `gopls` es el "cerebro" que da autocompletado, ir-a-definición, diagnósticos.
  La extensión de VS Code lo gestiona sola.

Tras instalar la extensión, abre el repo `Ellysia-Hygeia` en VS Code. Te
ofrecerá instalar herramientas adicionales (`gopls`, `dlv`, `staticcheck`):
di que sí.

---

## 3. Hola mundo y `go run`

Crea en cualquier sitio una carpeta `hola/` con un `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hola desde Go")
}
```

Inicializa un módulo y ejecuta:

```powershell
cd hola
go mod init ejemplo.com/hola
go run .
```

- `package main` + `func main()` = punto de entrada de un ejecutable.
- `go run .` **compila en memoria y ejecuta** sin dejar binario. Útil en
  desarrollo.
- `go build .` **compila y deja un binario** (`hola.exe` en Windows,
  `hola` en Linux/macOS) en la carpeta actual.

> Go **exige** que todo import se use y que no haya variables sin usar. Si
> importas `fmt` y no lo usas, no compila. Esto es intencional: código limpio.

---

## 4. Conceptos esenciales de Go

Lee esta sección con el esqueleto del repo abierto; verás cada concepto
aplicado.

### 4.1 Paquetes e imports

Cada carpeta es un paquete. El nombre del paquete va en la primera línea no
comentada de cada `.go`:

```go
package collector
```

Para usar algo de otro paquete, lo importas por su **ruta de módulo**:

```go
import "github.com/ProjectEllysia/Ellysia-Hygeia/payload"
```

Y lo referencias como `payload.Payload`, `payload.Metrics`, etc. Lo que
**empieza con mayúscula** se exporta (público); lo minúscula es privado del
paquete. No hay `public`/`private` como en Java: es la inicial.

### 4.2 Variables, tipos, constantes

```go
var x int        // cero-value = 0
var s string     // cero-value = ""
y := 42          // declaración corta con inferencia (solo dentro de funcs)
const Pi = 3.14  // constante
```

Tipos básicos: `int`, `int64`, `uint64`, `float64`, `string`, `bool`,
`time.Duration`, `error`. Los enteros sin signo (`uint64`) se usan en Hygeia
para bytes/tamaños (p. ej. `MemoryMetrics.TotalBytes`).

`:=` es la forma idiomática; `var` se usa sobre todo en paquete (fuera de
funcs) o cuando necesitas el cero-value explícito.

### 4.3 Funciones y múltiples retornos

```go
func add(a, b int) int { return a + b }

func dividir(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("división por cero")
	}
	return a / b, nil
}
```

Una función puede devolver **varios valores**. El patrón `(resultado, error)`
es ubicuo: devuelves `nil` como error si todo fue bien.

### 4.4 Errores: no hay excepciones

Go no tiene `try/catch`. Un `error` es un valor más:

```go
res, err := dividir(10, 0)
if err != nil {
	log.Error("no se pudo dividir", "err", err)
	return
}
fmt.Println(res)
```

La regla de oro: **después de cada llamada que pueda fallar, comprueba `err`**.
El compilador NO te obliga, pero es la convención más fuerte del lenguaje.
`errors.New("texto")` crea un error simple; `fmt.Errorf("...: %w", err)` envuelve
uno existente (conserva la cadena para `errors.Is`/`errors.As`).

### 4.5 Structs y métodos

Un `struct` agrupa campos. Un método es una función con un *receptor*:

```go
type CPUCollector struct{}

func (c *CPUCollector) Name() string { return "cpu" }
func (c *CPUCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// ...
}
```

`(c *CPUCollector)` es el receptor. `*` = puntero (puedes mutar el struct);
sin `*` = copia. Para implementaciones sin estado como `CPUCollector{}` da
igual, pero por convención se usa puntero.

### 4.6 Interfaces (satisfacción implícita)

```go
type Collector interface {
	Name() string
	Collect(ctx context.Context, m *payload.Metrics) error
}
```

**No declaras "implements"**. Si tu struct tiene los métodos con esa firma,
*ya* implementa la interfaz. Así, `CPUCollector`, `MemoryCollector`, etc. son
todos `Collector` sin decirlo en ningún sitio. Por eso el `Registry` guarda
`func() Collector` y puede mezclarlos.

> Esto es la **composición sobre la herencia**: cada colector es autónomo, y el
> bucle principal solo conoce la interfaz `Collector`.

### 4.7 Slices, maps y `range`

```go
names := []string{"cpu", "memory", "disk"}   // slice (lista dinámica)
for i, n := range names {                     // i = índice, n = elemento
	fmt.Println(i, n)
}

counters := map[string]int{"cpu": 1, "mem": 2}
for k, v := range counters {
	fmt.Println(k, v)
}
```

Los slices son la estructura de lista más común. `append(cs, x)` añade un
elemento. En `collector/collector.go`, `Build` hace `cs = append(cs, f())`.

### 4.8 Control de flujo

- **`if`** no lleva paréntesis: `if err != nil { ... }`.
- **`for`** es el único bucle: sirve como `while` y como `for` clásico.
  ```go
  for i := 0; i < 3; i++ { ... }   // clásico
  for x < 100 { x++ }              // estilo while
  for { ... }                      // infinito (se sale con break/return)
  ```
- **`switch`** sin `break` implícito necesario (Go ya no cae al siguiente caso
  salvo que uses `fallthrough`).

### 4.9 `defer`

`defer` ejecuta una llamada al **final** de la función, útil para limpieza:

```go
cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()   // garantiza liberar el contexto al salir
```

`defer` es LIFO (último en registrarse, primero en ejecutarse). Lo verás en
`main.go` (`defer stop()`, `defer ticker.Stop()`) y en cada goroutine del
`collectPayload`.

### 4.10 Goroutines, `sync.WaitGroup` y `context.Context`

Una **goroutine** es un hilo ligero: `go func(){ ... }()`.

```go
var wg sync.WaitGroup
for _, c := range cs {
	wg.Add(1)
	go func(c Collector) {
		defer wg.Done()
		// trabajo...
	}(c)
}
wg.Wait()   // bloquea hasta que todos terminan
```

- `wg.Add(1)` antes de lanzar; `defer wg.Done()` dentro.
- **Pasa `c` como argumento** a la goroutine (como en el esqueleto), no la
  captures en el closure: la variable de bucle se reutiliza y todas las
  goroutines verían el último valor. Pasarla como parámetro la fija.

`context.Context` es cómo Go cancela y pone plazos:

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
// pasa ctx a operaciones lentas; si se vence, ctx.Done() se cierra.
```

`signal.NotifyContext` (usado en `main.go`) devuelve un `ctx` que se cancela
al llegar Ctrl+C/SIGTERM. Así el agente cierra limpio.

### 4.11 JSON: `encoding/json`

Los *tags* entre backticks mapean campos a JSON:

```go
type Payload struct {
	AgentVersion string `json:"agentVersion"`
	CollectedAt  time.Time `json:"collectedAt"`
}
```

- `json.Marshal(p)` → `[]byte, error` (serializa).
- `json.Unmarshal(data, &target)` → deserializa.
- `omitempty` omite el campo si es el cero-value (vacío, 0, nil).
- Los campos deben empezar en mayúscula para ser exportables; el tag controla
  el nombre JSON.

En Hygeia, `payload/payload.go` es literalmente el contrato §9 convertido a
structs con tags. Serializar un `*Payload` produce exactamente el JSON que
espera el backend.

### 4.12 Logging estructurado: `log/slog`

```go
log := slog.New(slog.NewTextHandler(os.Stderr, nil))
log.Info("heartbeat enviado", "nextIntervalSec", 15)
log.Warn("colector falló", "name", "cpu", "err", err)
```

`slog` (stdlib desde Go 1.21) emite pares clave-valor. Es lo que usa el
esqueleto. Para JSON en vez de texto, cambia a `slog.NewJSONHandler`.

### 4.13 Punteros, en una frase

`&x` = dirección de `x`; `*p` = valor en esa dirección. En el esqueleto los
punteros aparecen en `m *payload.Metrics` (para mutar el struct compartido) y
en `*CPUMetrics` (para que `omitempty` funcione: un puntero nil se omite, un
puntero a struct vacío no).

---

## 5. Módulos y dependencias

Un **módulo** es un conjunto de paquetes versionados juntos. Se define en
`go.mod` (en la raíz del repo). El de Hygeia:

```
module github.com/ProjectEllysia/Ellysia-Hygeia

go 1.22
```

- `module` = ruta única de importación (usa la URL del repo).
- `go 1.22` = versión mínima de Go del módulo.

Comandos clave:

| Comando | Qué hace |
|---|---|
| `go mod init <ruta>` | Crea `go.mod` (ya hecho en este repo). |
| `go get github.com/shirou/gopsutil/v4` | Añade una dependencia. |
| `go mod tidy` | Añade lo que importas y quita lo que no usas; rellena `go.sum`. |
| `go mod download` | Descarga dependencias a la caché (sin tocar el código). |
| `go list -m all` | Lista todas las dependencias. |

`go.sum` es un fichero de **checksums** que garantiza que las dependencias no
han cambiado. **Se commitea** junto con `go.mod`.

El esqueleto actual **no tiene dependencias externas** (usa solo stdlib), por
eso no hay `require` en `go.mod` ni fichero `go.sum`. En cuanto añadas
`gopsutil` (§7.1), `go mod tidy` creará `go.sum`.

> **GOPRIVATE:** si algún día dependes de un repo privado de GitHub, configura
> `go env -w GOPRIVATE=github.com/ProjectEllysia/*` para que Go no consulte el
> proxy público.

---

## 6. Estructura del proyecto Hygeia (cómo leer el esqueleto)

```
Ellysia-Hygeia/
├── go.mod                  módulo: github.com/ProjectEllysia/Ellysia-Hygeia
├── main.go                 arranque, señales, bucle principal, fan-out paralelo
├── version.go              const AgentVersion (inyectable con -ldflags)
├── config.example.toml     ejemplo de config (cópialo a config.toml)
├── payload/
│   └── payload.go          tipos del contrato §9 (Payload, Metrics, ...)
├── config/
│   └── config.go           Config + Load (fichero TOML + override por env)
├── collector/
│   ├── collector.go        interfaz Collector + Registry
│   ├── cpu.go              CPUCollector
│   ├── memory.go           MemoryCollector
│   ├── disk.go             DiskCollector
│   ├── network.go          NetworkCollector
│   ├── processes.go        ProcessCollector
│   └── host.go             Host() -> payload.HostInfo
├── buffer/
│   └── buffer.go           RingBuffer en disco (resiliencia)
└── shipper/
    └── shipper.go          POST /ingest + gzip + backoff
```

### Por qué `payload/` es un paquete aparte

El contrato de ingesta (§9) lo comparten **tres** paquetes:
- `collector` rellena `Metrics`,
- `shipper` envía `Payload`,
- `buffer` almacena `Payload`.

Si los tipos vivieran en `collector`, entonces `shipper` y `buffer` dependerían
de `collector`, lo cual no tiene sentido (un "enviador" no debería conocer a los
"recolectores"). Al aislarlos en `payload/`, las dependencias forman un grafo
limpio sin ciclos:

```
payload  ◀──  collector
payload  ◀──  shipper      ◀──  main
payload  ◀──  buffer
config   ◀──  main
```

### El flujo de un ciclo (`main.go` → `runOnce`)

1. `collectPayload` lanza cada colector en su propia goroutine (paralelo), con
   un `context.WithTimeout` de 5 s por colector. Cada uno escribe un campo
   **distinto** de `p.Metrics`, así no hace falta mutex.
2. `shp.Send(ctx, p)` envía el heartbeat. Si falla, `buf.Push(p)` lo guarda.
3. Si el envío va bien, `drainBuffer` intenta vaciar lo acumulado.

### Dónde están los `TODO`

Cada `TODO` del esqueleto marca exactamente qué falta para la Fase 1 real:

| Archivo | TODO |
|---|---|
| `config/config.go` | parsear el fichero TOML (§7.2) |
| `collector/cpu.go` | leer CPU con gopsutil (§7.1) |
| `collector/memory.go` | leer memoria con gopsutil |
| `collector/disk.go` | leer disco con gopsutil |
| `collector/network.go` | leer red + calcular tasa (delta/tiempo) |
| `collector/processes.go` | procesos + top-N |
| `collector/host.go` | kernel + uptime con gopsutil |
| `shipper/shipper.go` | POST + gzip + backoff (§7.3) |
| `buffer/buffer.go` | ring en disco acotado (§7.4) |
| `main.go` | auto-ajustar el intervalo con `resp.NextIntervalSec` |

---

## 7. Implementar los TODO (gopsutil, TOML, shipper, buffer)

### 7.1 Añadir `gopsutil` y rellenar un colector

Primero, añade la dependencia y descárgala:

```powershell
go get github.com/shirou/gopsutil/v4
go mod tidy
```

Esto añade a `go.mod`:
```
require github.com/shirou/gopsutil/v4 v4.x.y
```
y crea `go.sum`. A partir de aquí, los subpaquetes se importan como
`github.com/shirou/gopsutil/v4/cpu`, `.../mem`, `.../disk`, `.../net`,
`.../process`, `.../host`, `.../load`.

**Ejemplo: `collector/cpu.go` real:**

```go
package collector

import (
	"context"
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
)

type CPUCollector struct{}

func NewCPU() Collector { return &CPUCollector{} }

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// Uso global y por core. cpu.Percent bloquea `interval` midiendo.
	perCore, err := cpu.Percent(time.Second, true)
	if err != nil {
		return err
	}
	global := 0.0
	for _, v := range perCore {
		global += v
	}
	if len(perCore) > 0 {
		global /= float64(len(perCore))
	}

	out := &payload.CPUMetrics{
		UsagePct:   global,
		PerCorePct: perCore,
	}

	// load average (en Windows load.Avg() suele devolver error: omíteme con gracia).
	if avg, err := load.Avg(); err == nil {
		out.LoadAvg = []float64{avg.Load1, avg.Load5, avg.Load15}
	}

	m.CPU = out
	return nil
}
```

Puntos clave:
- `cpu.Percent(time.Second, true)` **mide durante 1 s** (bloquea esa goroutine,
  pero como cada colector va en la suya, no bloquea a los demás).
- Si algo falla en una plataforma, **devuelve error** y el bucle lo loguea pero
  el agente no cae (README §5: "degrada con elegancia").
- El resto de colectores siguen el mismo patrón: importas el subpaquete de
  gopsutil, llamas a su función, mapeas al tipo de `payload`.

**Memoria** (`mem.VirtualMemory()` → `.Total`, `.Used`, `.UsedPercent`).
**Disco** (`disk.Partitions(true)` itera; `disk.Usage(p.Mountpoint)` por cada
uno; filtra `//`/loop en Linux).
**Red**: `net.IOCounters(true)` da **acumulados**; el contrato pide **tasa**.
Guarda el snapshot anterior en el struct del collector y calcula
`(actual - anterior) / segundos`. Por eso `NetworkCollector` debería tener
campos (p. ej. `last map[string]net.IOCountersStat` y `lastTime time.Time`).
**Procesos**: `process.Processes()`; por cada `p`: `p.Name()`,
`p.CPUPercent()`, `p.MemoryPercent()`; ordena y quédate con el top-N.
**Host**: `host.Info()` → `.KernelVersion`, `.Uptime`.

### 7.2 Parsear el fichero TOML (`config/config.go`)

Añade un parser TOML (`pelletier/go-toml/v2` es cómodo y respeta los tags
`toml:"..."` que ya pusimos en el `Config`):

```powershell
go get github.com/pelletier/go-toml/v2
go mod tidy
```

Y rellena `Load`:

```go
func Load(path string) (*Config, error) {
	c := &Config{
		IntervalSec: 15,
		BufferPath:  "hygeia-buffer.jsonl",
		Collectors:  []string{"cpu", "memory", "disk", "network", "processes"},
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := toml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("config: parseando %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: leyendo %q: %w", path, err)
	}
	if err := applyEnv(c); err != nil {
		return nil, err
	}
	// ... validaciones serverUrl / agentKey ...
	return c, nil
}
```

Ahora el flujo es: **fichero como base → entorno lo sobreescribe → validación**.
Mientras no exista `config.toml`, el agente sigue funcionando por env vars.

### 7.3 El shipper (`shipper/shipper.go`)

```go
func (s *Shipper) Send(ctx context.Context, p *payload.Payload) (*IngestResponse, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("shipper: marshal: %w", err)
	}

	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	if _, err := gzw.Write(body); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.serverURL, "/") + "/ingest"
	backoff := time.Second
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(gz.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+s.agentKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		resp, err := s.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			select {
			case <-time.After(backoff):
				backoff *= 2
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			select {
			case <-time.After(backoff):
				backoff *= 2
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("shipper: backend respondió %s", resp.Status)
		}
		var r IngestResponse
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("shipper: decode respuesta: %w", err)
		}
		return &r, nil
	}
	return nil, errors.New("shipper: agotados los reintentos")
}
```

Observa el patrón: el `ctx` se propaga a `http.NewRequestWithContext`, así un
SIGTERM cancela la petición en vuelo. El backoff duplica el espera en cada
intento (1 s → 2 s → 4 s). Nunca `InsecureSkipVerify: true` (README §5): el
`http.Client` por defecto ya valida TLS.

### 7.4 El buffer en disco (`buffer/buffer.go`)

La forma más sencilla y robusta: **JSONL** (un payload por línea) con un límite
de líneas. `Push` appenda; `Pop` lee la primera y la trunca. Una implementación
didáctica:

```go
func (b *RingBuffer) Push(p *payload.Payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	// TODO de verdad: si supera maxItems líneas, trunca las más viejas
	// (p. ej. reescribe el fichero sin las primeras N líneas).
	return nil
}
```

`0o600` = permisos restringidos (solo el dueño lee/escribe), coherente con
README §5. Para producción, un ring con head/tail en un fichero pre-asignado es
más eficiente, pero JSONL + truncar es suficiente para la Fase 2.

### 7.5 Auto-ajuste del intervalo (`main.go`)

El backend responde `{"nextIntervalSec": 30}`. Para aplicarlo sin re-desplegar,
`runOnce` debe poder reconstruir el `ticker`. Lo más limpio: sacar el ticker a
una variable que `runOnce` puede reemplazar, o usar un canal en vez de ticker.
Pequeño reto didáctico: prueba a sustituir `time.Ticker` por un `time.Timer`
que renueves con `Reset(nextInterval)` en cada iteración.

---

## 8. Compilación y cross-compilación

### 8.1 Los tres comandos básicos

| Comando | Resultado |
|---|---|
| `go run .` | compila en memoria y ejecuta (desarrollo). |
| `go build .` | deja el binario en la carpeta actual. |
| `go install ./...` | compila y lo instala en `$GOPATH/bin` (en PATH). |

El `.` significa "el paquete del directorio actual". `./...` significa "todos
los paquetes del módulo".

### 8.2 Binario estático (sin dependencias del SO)

```powershell
$env:CGO_ENABLED = "0"
go build -o hygeia.exe .
```

`CGO_ENABLED=0` desactiva el compilador de C → binario **totalmente estático**,
sin depender de libc. Para un agente que se copia a hosts mínimos, esto es lo
querido. En Linux:

```bash
CGO_ENABLED=0 go build -o hygeia .
```

### 8.3 Cross-compilación

Desde tu Windows puedes compilar para cualquier par `GOOS/GOARCH` sin instalar
nada extra (gracias a `CGO_ENABLED=0`):

```powershell
# Linux x86_64
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -o hygeia-linux-amd64 .

# Linux ARM64 (Raspberry Pi 4, servidores ARM)
$env:GOARCH = "arm64"
go build -o hygeia-linux-arm64 .

# macOS Apple Silicon
$env:GOOS = "darwin"; $env:GOARCH = "arm64"
go build -o hygeia-darwin-arm64 .

# Windows (restablece)
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -o hygeia.exe .
```

En Linux/macOS, en una sola línea:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o hygeia-linux-amd64 .
```

Pares comunes: `linux/amd64`, `linux/arm64`, `windows/amd64`,
`darwin/arm64`, `darwin/amd64`. Lista completa: `go tool dist list`.

> `gopsutil` es puramente Go y cross-compila sin problema con `CGO_ENABLED=0`.

### 8.4 Inyectar la versión con `-ldflags`

El esqueleto declara `const AgentVersion = "0.1.0-dev"` en `version.go`. En
CI/builds, sobreescribe su valor en compile-time sin tocar el código:

```powershell
go build -ldflags "-s -w -X main.AgentVersion=1.0.0" -o hygeia.exe .
```

- `-X main.AgentVersion=1.0.0` reescribe el valor del símbolo `main.AgentVersion`.
- `-s -w` quita tabla de símbolos e info de depuración → **binario más pequeño**
  (típicamente ~30 % menos). Úsalo para releases, no para depurar.

### 8.5 Reducir más el tamaño (opcional)

```bash
upx --best --lzma hygeia
```

`upx` comprime el ejecutable. Útil si el tamaño es crítico; ojo: algunos AV
marcan los binarios UPX. Para Hygeia (binario de unos MB) casi nunca hace
falta.

### 8.6 Formatear y revisar

```powershell
gofmt -w .              # formatea todos los .go (idempotente)
go vet ./...            # detecta errores comunes (shadowing, printf mal)
```

Instala `golangci-lint` (metalinter) para más comprobaciones:
```powershell
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run
```

VS Code con la extensión Go ya corre `gofmt` al guardar y `go vet` en vivo.

---

## 9. Testing y calidad

### 9.1 Escribir un test

Un test vive en un fichero `xxx_test.go` junto al código, en el mismo paquete:

```go
package collector

import (
	"context"
	"testing"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

func TestCPUCollector_FillsCPU(t *testing.T) {
	c := NewCPU()
	m := &payload.Metrics{}
	if err := c.Collect(context.Background(), m); err != nil {
		t.Fatalf("Collect falló: %v", err)
	}
	if m.CPU == nil {
		t.Fatal("se esperaba m.CPU relleno")
	}
}
```

### 9.2 Tabla de tests (patrón idiomático)

```go
func TestRegistry_Build(t *testing.T) {
	cases := []struct {
		names []string
		want  int
	}{
		{nil, 5},                       // por defecto, los 5
		{[]string{"cpu", "memory"}, 2},
		{[]string{"desconocido"}, 0},   // se ignora
	}
	r := NewRegistry()
	for _, tc := range cases {
		got := len(r.Build(tc.names))
		if got != tc.want {
			t.Errorf("Build(%v) = %d, want %d", tc.names, got, tc.want)
		}
	}
}
```

### 9.3 Ejecutar

```powershell
go test ./...            # todos los paquetes
go test -run TestCPU ./collector
go test -race ./...      # detector de data races (usa goroutines -> úsalo)
go test -cover ./...     # cobertura
go test -coverprofile=c.out ./... && go tool cover -html=c.out   # informe HTML
```

`-race` es **muy** recomendable en Hygeia: el `collectPayload` lanza goroutines
que escriben el struct compartido; `-race` detectaría si dos colectores
toquinasen el mismo campo.

---

## 10. Empaquetado como servicio

El binario solo hace peticiones salientes; el envoltorio del SO lo reinicia si
cae (README §6).

### 10.1 Linux — systemd

`/etc/systemd/system/hygeia.service`:
```ini
[Unit]
Description=Ellysia Hygeia agent
After=network-online.target

[Service]
ExecStart=/usr/local/bin/hygeia
Restart=always
RestartSec=5
User=hygeia
Environment=HYGEIA_SERVER_URL=https://ellysia.tu-dominio/hygeia
Environment=HYGEIA_AGENT_KEY=...

[Install]
WantedBy=multi-user.target
```
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now hygeia
journalctl -u hygeia -f
```

### 10.2 Windows — servicio

`github.com/kardianos/service` abstrae Windows/Linux/macOS con la misma API:

```powershell
go get github.com/kardianos/service
```

```go
type program struct{}
func (p *program) Start(s service.Service) error { go p.run(); return nil }
func (p *program) Stop(s service.Service) error  { return nil }
func (p *program) run() {
	// aquí va el mismo bucle de main.go, pero respondiendo a Start/Stop del SCM
}
```

### 10.3 macOS — launchd

Un `plist` en `/Library/LaunchDaemons/` con `KeepAlive=true`. `kardianos/service`
también lo genera.

---

## 11. Flujo de trabajo diario

1. **Arranca:** `go run .` (con `config.toml` o las env vars puestas).
2. **Mientras programas:** guarda en VS Code → `gopls` compila en vivo y
   subraya errores.
3. **Antes de commitear:**
   ```powershell
   gofmt -w .
   go vet ./...
   go test ./...
   go mod tidy
   ```
4. **Para un binario local:**
   ```powershell
   go build -o hygeia.exe .
   ```
5. **Para un release de otra plataforma:**
   ```powershell
   $env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="amd64"
   go build -ldflags "-s -w -X main.AgentVersion=1.0.0" -o hygeia-linux-amd64 .
   ```

> **Nunca commitees `config.toml`** con la `agentKey` real (ya está en
> `.gitignore`-pendiente — añádelo). El repo trae `config.example.toml` como
> plantilla; el `config.toml` real es por-host y no se versiona.

---

## 12. Recursos oficiales

- **Tour de Go** (interactivo, 2 h): https://go.dev/tour/ — empieza aquí.
- **Effective Go**: https://go.dev/doc/effective_go — cómo se *escribe* Go bien.
- **Referencia de la stdlib**: https://pkg.go.dev/std
- **gopsutil (v4)**: https://pkg.go.dev/github.com/shirou/gopsutil/v4
- **Cross-compilation**: https://go.dev/doc/install/source#environment
- **Módulos**: https://go.dev/ref/mod

### Orden sugerido para aprender, con este repo

1. Tour de Go (secciones 1–3): sintaxis básica.
2. Lee `payload/payload.go` y `config/config.go`: structs, tags, errores.
3. Tour (sección de concurrencia) + lee `main.go`: goroutines, `WaitGroup`,
   `context`.
4. Haz §7.1 (CPU con gopsutil) y compueba con `go run .` que el JSON sale.
5. Haz §7.3 (shipper) y prueba contra el backend (o un mock local).
6. Haz §7.4 (buffer) y prueba tirando el backend.
7. Cross-compila para Linux (§8.3) y despliega como servicio (§10).

---

*Esta guía acompaña al documento de diseño (`README.md`). Para la filosofía,
métricas, fases y el contrato de ingesta, consulta el README.*
