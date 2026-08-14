# Hygeia — documento de diseño del agente

> Por qué el agente es como es. Para instalarlo y usarlo, el
> [README](../README.md); para la costura con el backend,
> [`CONTRATO-INGESTA.md`](CONTRATO-INGESTA.md).

---

## 1. Filosofía: el agente es tonto a propósito

La **autoridad de detección vive en el backend**, no aquí. Así se cambian
umbrales y reglas sin re-desplegar el agente en decenas de hosts. El agente
solo:

1. **Se da de alta** (una vez): lee su clave de agente de la config.
2. **Recolecta** métricas cada N segundos.
3. **Empuja** un heartbeat al backend.
4. **Sobrevive** a que el backend no responda (buffer + reintento).
5. **Se auto-ajusta** al `nextIntervalSec` que responde el backend.

Lo que **NO** hace: no decide qué es una anomalía, no guarda histórico local
largo, no abre puertos de red (solo hace peticiones salientes → funciona
detrás de NAT sin exponer nada).

---

## 2. Por qué Go (decisión ya tomada, se conserva como registro)

| Opción | A favor | En contra | Veredicto |
|---|---|---|---|
| **Go** ✅ | Binario **estático único** sin runtime; cross-compila a Windows/Linux/macOS; huella mínima; `gopsutil` da CPU/mem/disco/red/procesos cross-platform de fábrica; es lo que usan node_exporter, Telegraf, el core de Datadog… | No es el stack del backend (Python) | **El agente real va en Go.** |
| **Rust** | Aún más pequeño/rápido (`sysinfo`) | Desarrollo más lento; no aporta sobre Go aquí | Solo si ya dominas Rust |
| **Python + `psutil`** | `psutil` hace *todo*; prototipo en una tarde | Distribuir necesita empaquetar el runtime (PyInstaller ≈ decenas de MB) | **Prototipo, no producto** |

El prototipo en Python que esta tabla contemplaba nunca llegó a hacerse: se fue
directo a Go.

---

## 3. Métricas

**Núcleo — implementado.** CPU (uso global y por núcleo, `loadAvg` en Unix),
memoria (total/usada/%, swap), disco (por punto de montaje: uso %, bytes
libres), red (por interfaz: tasa de bytes de entrada y salida, errores),
procesos (total, zombis, **top-5 por CPU y por memoria**). El top-N es lo que
convierte "CPU al 98 %" en "CPU al 98 % **por `xmrig`**".

**Inventario de software — implementado solo en Windows.** Lectura de las tres
ramas del registro donde Windows anota el software instalado. Linux y macOS son
funciones vacías (ver F-01 del análisis). Alimenta el informe en PDF y el
análisis de vulnerabilidades del motor Lybra.

**Pendiente de la lista original.** Tasas de entrada/salida de disco e IOPS,
número de conexiones activas, temperatura de sensores, usuarios conectados y
`ctxSwitches` (que hoy viaja siempre a cero). Desglosado como F-07 y A-11 en
[`ANALISIS-INGENIERIA.md`](ANALISIS-INGENIERIA.md).

**Señales de seguridad — no implementadas.** Puertos nuevos a la escucha,
procesos con CPU alta desde rutas sospechosas, recuento de inicios de sesión
fallidos. Es el diferencial de Ellysia y es la fase 7 del análisis (F-06). El
campo `localAlerts` del contrato existe para esto y todavía no lo rellena nadie.

---

## 4. Arquitectura interna

```
cmd/hygeia-agent/       main del servicio: subcomandos + envoltorio del SO
cmd/hygeia-tray/        main del companion de bandeja (§8)

internal/agent/         bucle principal + estado observable por el tray
internal/config/        carga de config (fichero + env), enrollment, permisos
internal/collector/     un colector por familia: cpu, mem, disk, net, proc
internal/buffer/        ring buffer en disco: resiliencia si el backend cae
internal/shipper/       POST /ingest, gzip, reintento con backoff
internal/payload/       los tipos del contrato de ingesta
internal/control/       canal local tray <-> servicio (§8)
internal/icon/          iconos de bandeja por estado
internal/autostart/     arranque del tray con la sesión
internal/logring/       últimas líneas de log en memoria, para GET /debug
internal/version/       agentVersion (va en cada payload)
```

Todo lo que no es un binario vive bajo `internal/`: es la única forma que tiene
Go de expresar "esto es detalle de implementación", y el compilador la hace
cumplir. El §4 original situaba `main.go` y `version.go` en la raíz; al
aparecer un segundo binario hicieron falta dos paquetes `main`, de ahí `cmd/`.

**El bucle.** `ticker` cada `intervalSec` → colectores en paralelo (goroutines)
con timeout individual → ensamblar payload → shipper. Si el POST falla, al
**buffer**. Al recuperar conexión, se drena de forma acotada: como mucho unos
pocos payloads por ciclo y respetando el suelo de cadencia del backend, para no
dejar de recolectar lo actual mientras se recupera lo viejo.

**El coste sobre el equipo vigilado importa.** Un agente de monitorización que
se nota es un agente que se desinstala. Tres decisiones concretas salen de ahí:
los colectores de CPU, red y procesos calculan tasas guardando la muestra del
ciclo anterior en vez de bloquear muestreando; el buffer en disco mantiene su
recuento en memoria y extrae por lotes, porque el icono de bandeja pregunta el
estado cada cinco segundos; y el log registra cambios de estado, no un heartbeat
correcto cada quince segundos.

---

## 5. Resiliencia y seguridad (aquí NO se recorta)

- **Sin pérdida de datos ni crecimiento sin límite:** buffer en disco
  **acotado** (ring); si se llena, descarta lo más viejo. Nunca RAM ni disco
  ilimitados. El fichero de log y el buffer de log en memoria siguen la misma
  regla.
- **TLS obligatorio** contra el backend; rechazar certificados inválidos (nada
  de `InsecureSkipVerify`).
- **Clave de agente** en fichero con permisos restringidos (0600 en Unix, DACL
  solo para SYSTEM/Administradores/dueño en Windows). Nunca en logs, ni
  siquiera al rechazarla.
- **Mínimo privilegio:** casi todas las métricas se leen sin root/admin; solo
  algún dato de proceso ajeno o la temperatura piden elevación. Se degrada con
  elegancia: se omite esa métrica, no se cae el heartbeat.
- **Auto-observación:** logs estructurados del propio agente, más un
  `GET /debug` en el canal de control con goroutines, memoria y las últimas
  líneas — para diagnosticar un agente mudo sin buscar el fichero de log.
- **Releases firmadas.** Pendiente (F-08). Auto-update es opcional y de nicho,
  descartado conscientemente para el beta (F-11).

---

## 6. Empaquetado y despliegue

Un binario por plataforma más el envoltorio de servicio del SO, abstraído por
`kardianos/service`: unit `systemd` en Linux, servicio de Windows, `launchd` en
macOS. Cross-compilación desde un solo `GOOS`/`GOARCH`, sin toolchains por
plataforma. Distribución = copiar un binario y un fichero de config.

En Windows hay además un instalador de doble clic
([`installer/build-installer.ps1`](../installer/build-installer.ps1)) que
empaqueta ambos binarios y un `config.toml` con el `serverUrl` ya relleno, para
que el cliente solo tenga que pegar su clave.

---

## 7. Autenticación

Cada activo tiene una **clave de agente** opaca, con la forma `keyId.secreto`,
emitida por el backend al dar de alta el activo (`POST /hygeia/assets`, se
muestra **una sola vez**). El agente la guarda en su config y la envía en cada
heartbeat como `Authorization: Bearer <agentKey>`. No hay login ni JWT: la
clave es la única identidad, y determina sobre qué activo puede escribir.

El backend guarda solo el `keyId` en claro (para el lookup) y un hash Argon2id
del secreto.

---

## 8. El companion de bandeja

`hygeia-tray` es opcional, no requiere privilegios y **no recolecta ni envía
métricas**: solo habla con el servicio por un canal de control local. Un
servidor sin escritorio corre `hygeia-agent` solo, exactamente igual.

El canal **no es TCP**: named pipe en Windows, socket Unix con permisos
restringidos en Linux y macOS. La autorización viene de los permisos del
transporte, no de confiar en que "localhost" es de fiar — localhost no
distingue qué usuario local se conecta. Encima de ese transporte se habla HTTP
normal.

Superficie deliberadamente mínima: `GET /status`, `GET /debug`, `POST /enroll`
y `POST /reset`. Nada de métricas de negocio, nada de parar o arrancar el
servicio.

Estados que reporta: `connected` (verde), `local_error` (rojo), `unconfigured`
(gris) y `starting` (gris, transitorio: hay clave pero todavía no se sabe si el
backend responde; decir `connected` ahí afirmaría un heartbeat que no ha
ocurrido).

**El enrollment solo se acepta sobre un agente sin configurar.** Así un usuario
local sin privilegios no puede pisar la clave de un activo ya dado de alta ni
reapuntar el agente a otro servidor. Para volver a empezar está `POST /reset`,
que borra la clave **local** — nunca la revoca en el backend, esa autoridad
sigue siendo del servidor.

---

## 9. Estado de las fases

| Fase | Entregable | Estado |
|---|---|---|
| **1** | Config + enrollment + colectores CPU/memoria/disco + shipper | ✅ |
| **2** | Red + procesos (top-N) + buffer en disco con reintento y backoff | ✅ |
| **3** | Servicio del SO (systemd/Windows/launchd) + releases firmadas | ✅ servicio · ❌ releases firmadas (F-08) |
| **4** | Señales de seguridad (puertos nuevos, cryptominer, logins fallidos) | ❌ (F-06) |
| **5** | Inventario de software | ✅ Windows · ❌ Linux y macOS (F-01) |

El plan de trabajo vigente, con las 30 oportunidades detectadas catalogadas por
impacto y esfuerzo y agrupadas en fases, está en
[`ANALISIS-INGENIERIA.md`](ANALISIS-INGENIERIA.md).
