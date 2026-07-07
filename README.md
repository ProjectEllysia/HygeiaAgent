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

**Rebanada mínima:** Fase 0 (Python) contra las Fases 0+1 del backend → ves un heartbeat
entrando en la DB. Luego Fase 1 en Go para el artefacto real.

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
