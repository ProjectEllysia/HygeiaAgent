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
  main.go                 # arranque, señales, bucle principal
  config/                 # carga de config (fichero + env), enrollment
  collector/              # un colector por familia: cpu, mem, disk, net, proc (gopsutil)
  buffer/                 # ring buffer en disco: resiliencia si el backend cae
  shipper/                # POST /hygeia/ingest, gzip, reintento con backoff
  version.go              # agentVersion (va en cada payload)
```

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

| Fase | Entregable |
|---|---|
| **0** | Prototipo Python+psutil: bucle → POST a `/hygeia/ingest`. Valida el backend. |
| **1** | Agente Go: config + enrollment + colectores CPU/mem/disco + shipper. |
| **2** | Red + procesos (top-N) + buffer en disco con reintento/backoff. |
| **3** | Servicio del SO (systemd/Windows/launchd) + releases firmadas. |
| **4** (opcional) | Señales de seguridad (puertos nuevos, cryptominer, logins fallidos). |
| **5** (opcional, ver §10) | Colector de inventario de software (paquetes instalados) + envío diferencial a `/hygeia/inventory`. |
| **6** (opcional, ver §11) | Companion de bandeja del sistema (`hygeia-tray`): estado del agente + enrollment con UI. |

**Rebanada mínima:** Fase 0 (Python) contra las Fases 0+1 del backend → ves un heartbeat
entrando en la DB. Luego Fase 1 en Go para el artefacto real.

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
GET  /status   → { "state": "connected"|"local_error"|"unconfigured", "lastPushAt": ..., "bufferSize": ... }
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
