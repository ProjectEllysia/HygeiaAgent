# Ellysia — Hygeia (agente)

> Agente ligero de monitorización de activos para **Ellysia**. Se instala en cada host,
> recolecta métricas de hardware (CPU, memoria, disco, red, procesos) cada N segundos y las
> empuja al backend de Ellysia (`POST /hygeia/ingest`).
>
> Hygeia (Ὑγίεια) es la diosa griega de la salud: este agente toma los **signos vitales**
> del activo y los reporta; quien decide si algo está "enfermo" es el backend.
>
> Este README es el documento de diseño del repositorio. El backend vive en el repo
> `Ellysia`; el **único acoplamiento** entre ambos es el *contrato de ingesta* (§9).

---

## 1. Filosofía: el agente es tonto a propósito

La **autoridad de detección vive en el backend**, no aquí. Así se cambian umbrales y reglas
sin re-desplegar el agente en decenas de hosts. El agente solo:

1. **Se da de alta** (una vez): lee su clave de agente de la config.
2. **Recolecta** métricas cada N segundos.
3. **Empuja** un heartbeat al backend.
4. **Sobrevive** a que el backend no responda (buffer + reintento).
5. **Se auto-ajusta** al `nextIntervalSec` que responde el backend.

Lo que **NO** hace: no decide qué es una anomalía, no guarda histórico local largo, no abre
puertos de red (solo hace peticiones salientes → funciona detrás de NAT sin exponer nada).

---

## 2. Lenguaje: **Go** (recomendado), con Python como prototipo desechable

| Opción | A favor | En contra | Veredicto |
|---|---|---|---|
| **Go** ✅ | Binario **estático único** sin runtime; cross-compila a Windows/Linux/macOS; huella mínima; `gopsutil` da CPU/mem/disco/red/procesos cross-platform de fábrica; es lo que usan node_exporter, Telegraf, el core de Datadog… | No es el stack del backend (Python) | **El agente real va en Go.** |
| **Rust** | Aún más pequeño/rápido (`sysinfo`) | Desarrollo más lento; no aporta sobre Go aquí | Solo si ya dominas Rust |
| **Python + `psutil`** | `psutil` hace *todo*; prototipo en una tarde | Distribuir necesita empaquetar el runtime (PyInstaller ≈ decenas de MB) | **Prototipo, no producto** |

> **Recomendación práctica:** escribe primero un **prototipo en Python+psutil** (~30 líneas:
> bucle → psutil → POST) para validar el flujo contra el backend en cuanto sus Fases 0+1
> estén. Pero el agente que **distribuyes e instalas** hazlo en **Go**: un binario que copias
> y arrancas como servicio gana a cualquier cosa con runtime. No inviertas en empaquetar
> Python para distribución — ese esfuerzo se tira al pasar a Go.

---

## 3. Métricas a recoger

**Núcleo (todas cross-platform con `gopsutil`):**

- **CPU** — uso % global y por core; `loadAvg` (1/5/15; en Windows se emula u omite);
  context switches.
- **Memoria** — total/usada/disponible/%; swap usado %.
- **Disco** — por punto de montaje: uso %, bytes libres; tasas IO read/write e IOPS.
- **Red** — por interfaz: bytes/paquetes in/out como **tasa** (no acumulado); errores y
  drops; nº de conexiones activas.
- **Procesos** — total; zombies; **top-N por CPU y por memoria** (pid, nombre, %). El top-N
  es lo que convierte "CPU al 98 %" en "CPU al 98 % **por `xmrig`**".
- **Sistema** — uptime, boot time, usuarios conectados, temperatura (si hay sensor).

**Señales de seguridad (opcional, diferencial de Ellysia — Fase 4):**

- Puertos nuevos a la escucha respecto al heartbeat anterior (persistencia/backdoor).
- Procesos con CPU alta sostenida y binario en rutas sospechosas (`/tmp`, `/dev/shm`) →
  olor a cryptominer.
- Recuento de logins fallidos (`auth.log` / Event Log) → fuerza bruta.

> Empieza por el núcleo. Las señales de seguridad aportan mucho, pero solo tienen sentido
> cuando el flujo base ya funciona.

**Inventario de software (opcional, sinergia con Themis/Lybra — ver §10):**

- Paquetes instalados (nombre, versión) vía el gestor nativo del SO: `dpkg -l`/`rpm -qa` en
  Linux, `winget list` o registro en Windows, `brew list` en macOS.
- A diferencia de todo lo anterior, **no es telemetría de heartbeat** — cambia poco y se
  envía por su propio camino, no cada `intervalSec`. Detalle completo en §10.

---

## 4. Arquitectura interna

```
hygeia-agent/
  cmd/hygeia-agent/       # main del servicio: subcomandos + envoltorio del SO
  cmd/hygeia-tray/        # main del companion de bandeja (§11)
  agent/                  # bucle principal + estado observable por el tray
  config/                 # carga de config (fichero + env), enrollment, permisos
  collector/              # un colector por familia: cpu, mem, disk, net, proc (gopsutil)
  buffer/                 # ring buffer en disco: resiliencia si el backend cae
  shipper/                # POST /hygeia/ingest, gzip, reintento con backoff
  control/                # canal local tray <-> servicio (§11.4)
  internal/icon/          # iconos de bandeja por estado
  internal/autostart/     # arranque del tray con la sesión (§11.1)
  version/                # agentVersion (va en cada payload)
```

> El §4 original situaba `main.go` y `version.go` en la raíz. Al aparecer un
> segundo binario (§11) hicieron falta dos paquetes `main`, de ahí `cmd/`, y
> la versión pasó a un paquete propio para poder compartirla entre ambos.

**Bucle:** `ticker` cada `intervalSec` → colectores en paralelo (goroutines) con timeout →
ensamblar payload (§9) → shipper. Si el POST falla, al **buffer** (ring en disco, tamaño
acotado). Al recuperar conexión, drena el buffer con backoff exponencial (evita martillear
un backend que se reinicia).

**Config (fichero TOML/YAML + override por env):**
```toml
serverUrl   = "https://ellysia.tu-dominio/hygeia"
agentKey    = "..."          # la clave emitida al dar de alta el activo en el backend
intervalSec = 15
collectors  = ["cpu","memory","disk","network","processes"]
```

---

## 5. Resiliencia y seguridad (aquí NO se recorta)

- **Sin pérdida de datos ni crecimiento sin límite:** buffer en disco **acotado** (ring);
  si se llena, descarta lo más viejo. Nunca RAM ni disco ilimitados.
- **TLS obligatorio** contra el backend; rechazar certificados inválidos (nada de
  `InsecureSkipVerify`).
- **Clave de agente** en fichero con permisos restringidos (0600 / ACL). Nunca en logs.
- **Mínimo privilegio:** casi todas las métricas se leen sin root/admin; solo algún dato de
  proceso ajeno o temperatura pide privilegios. Documenta qué necesita elevación y degrada
  con elegancia si no lo tiene (omite esa métrica, no crashees).
- **Auto-observación:** logs estructurados del propio agente (último push OK, tamaño del
  buffer, errores) para diagnosticar un agente mudo.
- **Releases firmadas.** Auto-update es opcional y de nicho — no en beta.
- **Integridad del binario en la instalación.** Cada release publica checksum (y firma); el
  instalador/usuario verifica el hash del binario **descargado** (§15 del plan de backend)
  antes de ejecutarlo. Un binario servido por la red no se ejecuta sin comprobar que es el
  que Ellysia firmó — es la contraparte, del lado del agente, del endurecimiento de la
  descarga.
- **Colectores que invocan al SO, sin shell.** El colector de inventario (§10) lanza
  `dpkg -l` / `rpm -qa` / `winget list` / `brew list` con **argumentos fijos** (`exec`/argv,
  nunca `sh -c` con interpolación) y **parsea su salida a la defensiva**: un nombre de paquete
  hostil (`; rm -rf`, bytes de control, líneas larguísimas) es *dato*, no puede romper el
  parseo ni inyectar comandos. Igual para el top-N de procesos: los nombres de proceso son
  datos no confiables, no formato.
- **Permisos del buffer.** El ring en disco (§4) hereda los mismos permisos restringidos que
  la config (0600 / ACL): contiene métricas del host, no debe quedar legible por otros
  usuarios locales.
- **Superficie saliente-solo.** El agente nunca abre un puerto de escucha para su función
  principal (§1); la única excepción es el canal de control local del companion de bandeja,
  endurecido aparte (§11.7).

---

## 6. Empaquetado / despliegue

Un binario por plataforma + el envoltorio de servicio del SO:

- **Linux:** unit `systemd` (`Restart=always`).
- **Windows:** servicio de Windows (p. ej. `kardianos/service`, abstrae los tres SO).
- **macOS:** `launchd`.

Cross-compilación desde un solo `GOOS/GOARCH` — sin toolchains por plataforma. Distribución
= copiar un binario + un fichero de config.

---

## 7. Fases

| Fase | Entregable | Estado |
|---|---|---|
| **0** | Prototipo Python+psutil: bucle → POST a `/hygeia/ingest`. Valida el backend. | omitida (se fue directo a Go) |
| **1** | Agente Go: config + enrollment + colectores CPU/mem/disco + shipper. | ✅ |
| **2** | Red + procesos (top-N) + buffer en disco con reintento/backoff. | ✅ |
| **3** | Servicio del SO (systemd/Windows/launchd) + releases firmadas. | ✅ servicio · ❌ releases firmadas |
| **4** (opcional) | Señales de seguridad (puertos nuevos, cryptominer, logins fallidos). | ❌ |
| **5** (opcional, ver §10) | Colector de inventario de software (paquetes instalados) + envío diferencial a `/hygeia/inventory`. | ❌ |
| **6** (opcional, ver §11) | Companion de bandeja del sistema (`hygeia-tray`): estado del agente + enrollment con UI. | ✅ |

**Rebanada mínima:** Fase 0 (Python) contra las Fases 0+1 del backend → ves un heartbeat
entrando en la DB. Luego Fase 1 en Go para el artefacto real.

> Con 1-3 y 6 en pie, el trabajo pendiente ya no son fases nuevas — es la pasada de calidad
> del **§12, Etapa 2**.

---

## 8. Autenticación

Cada activo tiene una **clave de agente** opaca, emitida por el backend al dar de alta el
activo (`POST /hygeia/assets`, se muestra **una sola vez**). El agente la guarda en su
config y la envía en cada heartbeat como `Authorization: Bearer <agentKey>`. No hay login
ni JWT: la clave es la única identidad, y determina sobre qué activo puede escribir.

---

## 9. Contrato de ingesta (la costura con el backend)

Fuente autoritativa: el plan del backend (`plans/hygeia-asset-monitoring-backend.md` en el
repo `Ellysia`). Reproducido aquí para que este repo sea autocontenido. Claves **camelCase**.

`POST {serverUrl}/ingest` · `Authorization: Bearer <agentKey>` · JSON (gzip recomendado):

```jsonc
{
  "agentVersion": "1.0.0",
  "collectedAt": "2026-07-07T10:00:00Z",   // ISO-8601 UTC, reloj del agente
  "host": {
    "hostname": "web-01",
    "os": "linux",
    "kernel": "6.1.0",
    "uptimeSec": 123456
  },
  "metrics": {
    "cpu":    { "usagePct": 87.5, "loadAvg": [2.1, 1.8, 1.5], "ctxSwitches": 12345,
                "perCorePct": [88, 91, 80, 90] },
    "memory": { "totalBytes": 8589934592, "usedBytes": 7300000000, "usagePct": 85.0,
                "swapUsedPct": 12.0 },
    "disk":   [ { "mount": "/", "usagePct": 91.2, "freeBytes": 5000000000 } ],
    "network":[ { "iface": "eth0", "rxBytesPerSec": 120000, "txBytesPerSec": 45000,
                  "errIn": 0, "errOut": 0 } ],
    "processes": { "total": 210, "zombie": 1,
                   "topCpu": [ { "pid": 8123, "name": "xmrig", "cpuPct": 96.0 } ],
                   "topMem": [ { "pid": 990, "name": "java", "memPct": 22.0 } ] }
  },
  "localAlerts": []   // opcional: anomalías pre-marcadas (la autoridad es el servidor)
}
```

Respuesta del backend (úsala para auto-ajustar el intervalo sin re-desplegar):

```jsonc
{ "ok": true, "nextIntervalSec": 15, "serverTime": "2026-07-07T10:00:01Z" }
```

**Reglas del contrato:**
- El backend valida y **descarta lo desconocido**; enviar campos de más no rompe nada, pero
  tampoco se persiste.
- La identidad del activo la determina la **clave**, nunca un `assetId` del payload.
- Versiona el payload con `agentVersion`; congela el esquema pronto y evoluciona por
  extensión (campos nuevos opcionales), no por ruptura.

---

## 10. Inventario de software — sinergia con Themis/Lybra (opcional)

> Sección hermana de la §14 del plan de backend
> (`plans/feature/hygeia/hygeia-backend.md`). Ahí está el porqué completo: Themis/Lybra ve
> el activo desde la red (fingerprinting inferido, sujeto a backports); Hygeia, al vivir
> dentro del host, puede darle al matcher CPE→CVE de Lybra la versión **real** del paquete
> instalado, sin necesidad de que Lybra abra una sesión SSH con credencial de Acheron (la
> Fase 4 del roadmap del motor, "escaneo autenticado"). Esta sección cubre solo la parte que
> vive en el agente.

**Qué recolecta el colector nuevo** (mismo principio del §1: el agente es tonto, solo junta
datos — no decide qué es vulnerable, eso es trabajo del matcher en el backend):

- Listado de paquetes instalados vía el gestor nativo: `dpkg -l`/`rpm -qa` en Linux,
  `winget list`/registro en Windows, `brew list` en macOS. Por paquete: nombre y versión
  (el `vendor`/CPE lo resuelve el backend, igual que hace hoy con los servicios de red —
  no dupliques esa lógica de normalización aquí).
- Versión de kernel/SO — ya viaja en el bloque `host` del contrato de ingesta (§9); no hace
  falta duplicarla.

**Ruta y cadencia — distinta de la del heartbeat:**

- No es telemetría de intervalo corto. Un endpoint propio, `POST {serverUrl}/inventory`, con
  la misma cabecera `Authorization: Bearer <agentKey>` del heartbeat (§8) — mismo mecanismo
  de identidad, sin JWT, sin superficie nueva de auth.
- **Envío diferencial.** El agente calcula un hash del listado de paquetes y solo lo reenvía
  si cambió desde el último envío (o, como tope, una vez al día aunque no cambie, para que el
  backend sepa que el agente sigue vivo y su vista de inventario no está simplemente
  desactualizada). Evita mandar varios MB de listado de paquetes cada 15 s por nada —el
  mismo motivo por el que el bucle principal (§4) no toca este colector.

```jsonc
// POST {serverUrl}/inventory
{
  "agentVersion": "1.0.0",
  "collectedAt": "2026-07-13T10:00:00Z",
  "packages": [
    { "name": "openssl", "version": "1.1.1f" },
    { "name": "apache2", "version": "2.4.49" }
  ]
}
```

> **ponytail: no construyas esto antes de que el flujo base (Fases 0-2) esté probado en
> producción.** Es una extensión, no un prerrequisito — el agente sin este colector sigue
> siendo completamente útil para su propósito original (monitorización de salud).

---

## 11. Companion de escritorio: icono de bandeja del sistema (opcional)

> Fase 6. No es parte del agente en sí — es un **segundo binario** que vive en la sesión de
> escritorio del usuario, separado del servicio headless (§6). Se construye después de que
> las Fases 0-3 estén estables; no bloquea nada de lo anterior.

### 11.1 Por qué es un binario aparte

El servicio (`hygeia-agent`) corre como `systemd`/servicio de Windows/`launchd`, casi siempre
sin sesión de escritorio (arranca antes del login, o bajo una cuenta de sistema sin GUI). Un
icono en la bandeja **necesita** una sesión de usuario con escritorio activo. Mezclar ambas
cosas en un solo binario ataría el agente (que debe poder correr en un servidor headless) a
tener siempre un entorno gráfico disponible — rompe el caso de uso más común (servidores).

Por tanto: `hygeia-tray`, un **segundo binario ligero** que se instala solo en las máquinas
donde tiene sentido (estaciones de trabajo, laptops), arranca con la sesión del usuario
(entrada de inicio de sesión / `Startup` en Windows, `LaunchAgent` en macOS, `.desktop` con
`XDG_AUTOSTART` en Linux), y **no recolecta ni envía métricas** — solo consulta el estado del
servicio y, si hace falta, le pasa la clave de agente.

### 11.2 Qué muestra el icono

Tres estados mínimos, cada uno con su icono:

- 🟢 **Conectado** — el último heartbeat al backend de Ellysia tuvo éxito.
- 🔴 **Error local** — el agente no puede recolectar métricas (colector caído, sin permisos)
  o el buffer en disco (§5) está creciendo porque el backend no responde tras varios
  reintentos.
- ⚪ **Sin configurar** — no hay `agentKey` en la config: el agente está instalado pero no
  dado de alta contra ningún activo todavía.

El menú del icono (click) muestra un resumen breve (última recolección OK, tamaño del
buffer, versión) y, si aplica, la opción de **introducir la clave de agente**.

### 11.3 Enrollment sin clave — mini UI

Si el servicio arranca y no encuentra `agentKey` en su config, sigue vivo (no crashea) pero
se queda en estado "sin configurar" sin intentar recolectar/enviar nada. El tray lo detecta
y, en vez de forzar al usuario a editar el TOML a mano, ofrece una ventanita mínima (un solo
campo + botón "Guardar") para pegar la clave que el backend mostró al dar de alta el activo
(§8, se muestra una sola vez). Tras guardarla, el servicio recarga la config (o se reinicia)
y arranca el bucle normal.

### 11.4 Cómo habla el tray con el servicio

El tray **no** escribe directamente el fichero de config del servicio (permisos: en Linux el
servicio puede correr como usuario distinto al de sesión; en Windows como
`LocalSystem`/servicio dedicado). En su lugar, el servicio expone un **socket de control
local, solo loopback**:

- Linux/macOS: socket Unix (`/run/hygeia-agent.sock` o equivalente en el directorio de
  estado del servicio), permisos restringidos al grupo del servicio.
- Windows: named pipe.
- Alternativa cross-platform más simple de implementar (menos idiomática): HTTP en
  `127.0.0.1:<puerto>` con un token local generado al arrancar y compartido solo vía
  fichero con permisos restringidos — nunca expuesto fuera de loopback.

Superficie mínima de ese control channel (no es la API de Ellysia, es interna
tray↔servicio):

```
GET  /status   → { "state": "connected"|"local_error"|"unconfigured"|"starting", "lastPushAt": ..., "bufferSize": ... }
POST /enroll   → { "agentKey": "..." }   # el servicio la persiste en su propia config, con sus propios permisos
```

Esto mantiene el principio del §1 (el agente es tonto) y el de mínimo privilegio (§5): el
tray solo lee estado y empuja una clave; nunca toca métricas ni decide nada.

### 11.5 Stack sugerido

Igual que el agente, en Go para reusar toolchain y cross-compilación:
[`getlantern/systray`](https://github.com/getlantern/systray) (o `fyne.io/systray`) para el
icono nativo en las tres plataformas; una ventana nativa mínima (o un diálogo del propio
toolkit) para el formulario de enrollment — no hace falta un framework de UI pesado para un
campo de texto y un botón.

### 11.6 Qué NO hace el tray

- No recolecta métricas ni las envía — eso sigue siendo trabajo exclusivo del servicio.
- No decide estados de salud del activo (esa autoridad, como en todo el diseño, es del
  backend) — solo refleja si el *agente local* está pudiendo hablar con Ellysia o no.
- No es un requisito para que el agente funcione: un servidor sin sesión de escritorio
  corre `hygeia-agent` solo, sin `hygeia-tray`, exactamente igual que hoy.

### 11.7 Endurecimiento del canal de control (tray ↔ servicio)

El socket de control del §11.4 es **superficie de ataque local nueva** — cualquier proceso o
usuario de la misma máquina podría intentar hablar con él. Reglas para que no se convierta en
una vía de escalada:

- **Autorización por permisos del transporte, no por confiar en `localhost`.** Un socket Unix
  / named pipe con permisos restringidos (dueño = cuenta del servicio, o un grupo dedicado) es
  preferible a HTTP en loopback **precisamente** porque `127.0.0.1` no distingue qué usuario
  local se conecta: en una máquina multiusuario, cualquiera puede abrir un socket a
  `127.0.0.1`. Si aun así se opta por loopback HTTP por simplicidad, exigir el token local del
  §11.4 (fichero con permisos 0600) en cada petición — sin token, `401`.
- **`POST /enroll` valida y no reconfigura a ciegas.** El servicio comprueba el **formato** de
  la clave (`keyId.secreto`, longitudes esperadas — §4 del plan de backend) antes de
  persistirla, y solo acepta el enrollment cuando está **sin configurar** (o exige el token
  local para sobrescribir una clave existente). Así un usuario local sin privilegios no puede
  reapuntar el agente a otro servidor ni pisar la clave de un activo ya dado de alta.
- **El canal nunca sale de la máquina.** Ni el socket Unix, ni el named pipe, ni el loopback
  se exponen en ninguna interfaz de red. Tray y servicio viven en el mismo host por
  definición.
- **Superficie mínima.** El canal de control **no** expone métricas, ni permite parar/arrancar
  el servicio, ni nada más allá de `GET /status` + `POST /enroll` (§11.4). Cuanto menor la
  superficie, menos que endurecer.

---

## 12. Etapa 2 — mejoras de calidad (post Fases 1-3 + 6)

Con las Fases 1-3 y 6 en pie (§7), esta etapa no añade fases nuevas: es una pasada de
calidad sobre el código ya escrito, más un puñado de extensiones concretas. Cada propuesta
lleva dos etiquetas — **Esfuerzo** (Bajo/Medio/Alto) e **Impacto** (Bajo/Medio/Alto) — y una
tercera, **Backend**, que dice si toca el contrato de ingesta o cualquier otra superficie del
repo `Ellysia`. Todo lo marcado `Backend: No` se puede implementar y desplegar sin coordinar
nada con el otro repo. §12.4 recopila lo que sí lo toca, para trasplantarlo tal cual al plan
del backend.

### 12.1 Hallazgo destacado: `topCpu` no mide lo que promete el §3

El §3 vende el top-N de procesos como lo que "convierte 'CPU al 98 %' en 'CPU al 98 % **por
`xmrig`**'". Auditando `collector/processes.go` contra la implementación real de
`gopsutil/v4` (`process.Process.CPUPercentWithContext`, `process/process.go:363-380`), el
cálculo es:

```go
totalTime := time.Since(created).Seconds()   // desde que el proceso ARRANCÓ
return 100 * cput.Total() / totalTime
```

Es decir: **media de CPU desde que el proceso se creó**, no uso reciente. Un proceso que
lleva días corriendo tranquilo y empieza a consumir el 100 % ahora mismo seguiría reportando
un `cpuPct` casi cero en el heartbeat — la media de días de inactividad diluye el pico. Esto
no es un detalle menor: es el escenario exacto que el §3 usa como argumento de venta del
top-N (un proceso preexistente que se vuelve malicioso), y hoy no lo detecta.

`MemoryPercent` no tiene este problema — es instantáneo (`RSS actual / total`), así que
`topMem` está bien tal como está.

**Arreglo:** el mismo patrón que ya usa `collector/network.go` (guardar una muestra anterior
y calcular el delta) — aquí por PID en vez de por interfaz:

```go
type ProcessCollector struct {
    mu   sync.Mutex
    prev map[int32]cpuSample   // pid -> (cpuTimeTotal, wallClock) del ciclo anterior
}
```

En cada `Collect`, calcular `cpuPct = 100 * (cput.Total() - prev.total) / elapsed` para los
PIDs presentes en ambas muestras; un proceso nuevo (sin muestra anterior) se omite de
`topCpu` ese primer ciclo, igual que ya hace `network.go` con interfaces nuevas. El campo
`cpuPct` del contrato no cambia de forma ni de tipo — solo empieza a significar lo que el
README dice que significa. **Backend: No.**

- **Esfuerzo:** Bajo — el patrón ya existe en el repo, es trasplantarlo.
- **Impacto:** Alto — corrige la funcionalidad estrella del colector de procesos.

### 12.2 Priorización

#### Tier 1 — alto impacto, bajo esfuerzo (hacer ya)

| Mejora | Backend | Estado |
|---|---|---|
| **§12.1** Corregir `topCpu` (media histórica → tasa reciente, patrón de `network.go`) | No | ✅ |
| **Mutex interno en `buffer.RingBuffer`.** `agent.Status()` (goroutine del canal de control) llama a `buf.Len()` mientras el bucle principal (otra goroutine) llama a `buf.Push()`/`buf.Pop()` sin ninguna sincronización compartida — `agent.go` protege sus propios campos con `a.mu`, pero `Push`/`Pop` se invocan fuera de ese lock (`runOnce`/`drainBuffer`). El propio `RingBuffer` debe ser thread-safe por diseño (es la convención Go para un tipo de uso concurrente), no depender de que el caller lo serialice correctamente — y hoy no lo hace. | No | ✅ |
| **Jitter de arranque** antes del primer tick (`rand` proporcional a `intervalSec`). Sin él, un reinicio masivo de flota (corte eléctrico, actualización de Windows) hace que todos los agentes golpeen `/ingest` en el mismo segundo. | No | ✅ |

#### Tier 2 — limpieza rápida (bajo esfuerzo, mismo sprint que el Tier 1)

| Mejora | Backend | Estado |
|---|---|---|
| **`Stop()` no espera al canal de control.** En `cmd/hygeia-agent/main.go`, `program.Stop` cancela el contexto y espera `p.done` (el bucle del agente), pero la goroutine de `srv.Serve(ctx)` no tiene su propio `done` y nadie la espera — el servicio puede reportarse "parado" con el pipe/socket todavía cerrándose. Añadir un segundo canal y esperar ambos. | No | ✅ |
| **`processes.go` recrea el proceso tras ordenar.** `toProcessInfo` llama a `gpsproc.NewProcessWithContext(pid)` para leer el nombre de los top-N, en vez de reusar el `*process.Process` que ya se obtuvo en el primer paso (y que ya se sabe que existe) — una llamada redundante que revalida el PID. Guardar el puntero en `procSample` y llamar `.NameWithContext` directamente sobre él. | No | ✅ *(resuelto de paso al arreglar §12.1)* |
| **Eliminar `shipper.IngestResponse.NextInterval()`.** Cero llamadas en todo el repo (`agent.go` lee `resp.NextIntervalSec` directamente) — código muerto, violación de YAGNI/LEAN. | No | ✅ |
| **`disk.isPseudoMount` reimplementa `strings.HasPrefix` a mano** con aritmética de índices. Sustituir por `strings.HasPrefix(m.Mountpoint, p+"/")` — mismo comportamiento, sin la reimplementación. | No | ✅ |
| **Validación defensiva en `config.Load`:** tope superior a `intervalSec` (un typo tipo `1500000` no debería dejar el agente mudo un mes) y guardas `math.IsNaN`/`IsInf` en `round1` antes del cast a `int64` (hoy indefinido si algún colector llegara a pasar un valor no numérico). | No | ✅ |
| **Extraer el dispatcher de subcomandos** (`runCommand`, `statusName`, `usage`) de `cmd/hygeia-agent/main.go` a un fichero propio — `main.go` mezcla arranque del servicio con parsing de CLI; separarlo es una mejora de SRP de coste casi nulo. | No | ✅ |

#### Tier 3 — alto impacto, esfuerzo medio (siguiente sprint)

| Mejora | Backend |
|---|---|
| **Cobertura de tests para `buffer`, `shipper` y los helpers puros de `collector`.** Hoy son 0 %: `buffer` es el componente que sostiene la promesa de "sin pérdida de datos" del §5 y no tiene ni un test de eviction ni de recuperación ante línea corrupta; `shipper` (retries, backoff, gzip, headers) se puede testear entero con `httptest.NewServer` y hoy no tiene ningún test; en `collector`, `isPseudoMount`, `rate()` (wrap-around de contadores) y el ordenamiento de `toProcessInfo` son funciones puras, triviales de testear por tabla, y no lo están. | No |
| **CI** (`go build` + `go vet` + `go test` en Windows/Linux/macOS vía GitHub Actions). Varios de los bugs de esta misma auditoría (el ACL de Windows que no restringía nada, el named pipe sin override que hacía que los tests hablaran con un agente real) se habrían detectado o al menos hecho más visibles con una matriz de CI corriendo en cada push. macOS necesita el runner con Xcode (ya lo traen los `macos-latest` de GitHub) para compilar `hygeia-tray` (cgo vía `systray`); Windows no necesita toolchain extra. | No |
| **Endpoint de depuración local** sobre el canal de control ya existente (§11.4) — no una superficie nueva, una ruta más (`GET /debug`) en el mismo `control.Server`, protegida por el mismo transporte endurecido en §11.7. Contenido: goroutines activas, `runtime.MemStats` del propio proceso, y las últimas N líneas de log en memoria (un ring buffer pequeño de texto, no de payloads). Pensado para un futuro `hygeia-agent debug` que se conecta al canal y vuelca ese estado por consola — diagnóstico de campo sin depender de encontrar el fichero de log. | No |

#### Tier 4 — impacto medio, esfuerzo medio

| Mejora | Backend |
|---|---|
| **Recursos propios del agente en el payload** (`agentSelf`: CPU%, RSS, goroutines del propio proceso `hygeia-agent`). Permite al backend distinguir "el host está mal" de "el agente está mal" — hoy, si `hygeia-agent` tiene una fuga de memoria o se dispara en CPU, nada en el heartbeat lo refleja. Campo opcional y aditivo (§9: "evoluciona por extensión"). | **Sí** |
| **Contador de payloads descartados por el buffer** (`bufferDroppedSinceLastPush`), incrementado cada vez que `RingBuffer.Push` tira el ítem más viejo por estar lleno, reseteado tras un envío exitoso. Hoy esa pérdida es visible localmente en el tray (§11.2, tamaño del buffer) pero el backend no tiene ninguna señal de que ocurrió durante un corte — ve un hueco temporal en los datos y no sabe si fue por eso o por otra cosa. | **Sí** |
| **Hot-reload de config** (`fsnotify` sobre `config.toml`): aplicar en caliente cambios de `intervalSec`/`collectors` sin reiniciar el servicio. `agentKey`/`serverUrl` siguen requiriendo reinicio explícito. Requiere cuidado con `agent.mu` para no pisar una escritura concurrente de `Enroll()`. | No |

#### Tier 5 — grandes apuestas (alto esfuerzo, priorizadas por decisión explícita)

| Mejora | Backend |
|---|---|
| **Rediseño del ring buffer.** El formato actual (JSONL de fichero único) reescribe el fichero entero en cada `Push`/`Pop` — O(n) por operación. Con el tope de 1000 ítems el peor caso está acotado, pero un corte de red de horas con un `intervalSec` bajo puede acercarse a ese tope y encadenar reescrituras cada vez más caras. Propuesta: log segmentado tipo WAL — ficheros `segment-NNNN.jsonl` de tamaño fijo (p. ej. 100 payloads); `Push` hace *append* O(1) al segmento activo, abriendo uno nuevo cuando se llena; `Pop` lee del segmento más antiguo y lo borra entero (también O(1)) cuando queda vacío; la eviction del ring borra el segmento más antiguo completo al superar el máximo. La interfaz pública (`Push`/`Pop`/`Len`) no cambia — `agent.go` no se entera del cambio de formato, es una mejora interna de encapsulación (SOLID: los consumidores dependen de la interfaz). Alternativa con menos código propio pero una dependencia nueva: `go.etcd.io/bbolt` como KV embebido con clave autoincremental. Empezar por el log segmentado antes de traer una dependencia nueva (LEAN). | No |
| **Firma de releases + verificación de integridad** (cierra el pendiente de la Fase 3, §7). Del lado del agente, concretamente: (1) cada release publica `SHA256SUMS` y una firma (`minisign` o `cosign` keyless); (2) el agente embebe la clave pública de verificación (mismo mecanismo que `internal/icon` usa para embeber el mark de marca, aquí para una clave); (3) un subcomando `hygeia-agent verify <binario>` que un instalador o un operador ejecuta contra el checksum/firma publicados antes de sustituir el binario en producción. El MVP (verificar un binario ya descargado a mano desde GitHub Releases) **no toca el backend en absoluto**. Solo pasaría a tocarlo si más adelante se añade auto-update con un `GET /hygeia/agent/latest` que el agente consulte — eso es una decisión aparte, no un prerrequisito de esta. | No (Sí solo si se añade auto-update con consulta al backend) |

### 12.3 Descartado o diferido en esta etapa

- **Cgroup/container awareness** (límites efectivos de CPU/memoria dentro de un contenedor,
  en vez de los del host). Cambiaría el contrato de ingesta y presupone que Hygeia está
  pensado para correr containerizado, algo que no está decidido — queda fuera de la Etapa 2
  hasta que ese alcance se confirme explícitamente. *(descartado)*
- **Validar que `serverUrl` sea `https://`** (rechazar `http://` en `config.Load`, para que
  el §5 "TLS obligatorio" no dependa solo de buena voluntad). Hoy no hay ninguna
  comprobación de esquema — se descubrió durante la verificación en caliente del Tier 1,
  contra un servidor de prueba en `http://127.0.0.1`, que el agente lo aceptó sin queja.
  **Diferido, no descartado:** el entorno de desarrollo actual sirve el backend (SPA) detrás
  de Vite, que no sirve con certificado — forzar `https://` ahora mismo rompería ese flujo
  de desarrollo. Retomar cuando el entorno de dev tenga TLS (o, alternativa más barata:
  permitir `http://` solo cuando el host sea `localhost`/`127.0.0.1`, y exigir `https://`
  para cualquier otro). *(diferido)*

### 12.4 Para trasplantar al plan del backend

Todo lo que en las tablas de arriba dice `Backend: Sí`, consolidado:

1. **`agentSelf`** — bloque opcional nuevo en `POST /ingest` con CPU%/RSS/goroutines del
   propio proceso `hygeia-agent` (Tier 4, §12.2).
2. **`bufferDroppedSinceLastPush`** — campo opcional nuevo en `POST /ingest`, contador de
   payloads perdidos por el ring buffer desde el último envío exitoso (Tier 4, §12.2).
3. *(condicional, no decidido)* Si se opta por auto-update del agente: un endpoint tipo
   `GET /hygeia/agent/latest` que devuelva versión/checksum/firma vigentes (Tier 5, firma de
   releases, §12.2) — no es parte del MVP de esa mejora, solo su extensión natural si se
   decide más adelante.

Ninguno rompe el contrato existente: los tres son campos/endpoints aditivos (§9, "evoluciona
por extensión, no por ruptura").
