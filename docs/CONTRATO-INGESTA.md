# Contrato de ingesta de Hygeia — v1.2

> **Este documento es la fuente única del contrato entre el agente Hygeia y el
> backend de Ellysia.** Antes existía por duplicado, en el §9 del README de este
> repositorio y en el §11 del plan del backend
> (`EllysiaServer/plans/feature/hygeia/hygeia-backend.md`). Las dos copias
> divergieron —la del README nunca llegó a mencionar el campo `inventory`, que
> lleva semanas en producción— y de esa divergencia salieron tres fallos reales
> de pérdida de datos, documentados en
> [`ANALISIS-INGENIERIA.md`](ANALISIS-INGENIERIA.md) como A-01, A-02 y A-04.
>
> Quien cambie este documento cambia la costura entre los dos repositorios. Los
> dos lados deben enlazar aquí, no copiar.

**Implementado por:**

| Lado | Fichero |
|---|---|
| Agente (tipos Go) | [`internal/payload/payload.go`](../internal/payload/payload.go) |
| Backend (validación) | `EllysiaServer/API/src/modules/features/hygeia/schemas.py` |
| Backend (límites) | `EllysiaServer/API/SecOpsConfig.json`, bloque `features.hygeia.limits` |

---

## 1. La petición

```
POST {serverUrl}/ingest
Authorization: Bearer <agentKey>
Content-Type: application/json
Content-Encoding: gzip          (opcional pero recomendado; el agente siempre comprime)
```

La identidad del activo la determina **la clave, nunca un identificador del
cuerpo**. Un agente solo puede escribir sobre su propio activo.

La cabecera `Content-Length` es **obligatoria**: el backend rechaza con `413`
cualquier petición que no la traiga, en vez de intentar adivinar cuánto leer.

### Cuerpo

```jsonc
{
  "agentVersion": "1.0.4",
  "collectedAt": "2026-08-13T10:00:00.123456Z",
  "host": {
    "hostname": "web-01",
    "os": "linux",
    "kernel": "6.1.0",
    "uptimeSec": 123456
  },
  "metrics": {
    "cpu": {
      "usagePct": 87.5,
      "loadAvg": [2.1, 1.8, 1.5],
      "ctxSwitches": 0,
      "perCorePct": [88.0, 91.2, 80.4, 90.1]
    },
    "memory": {
      "totalBytes": 8589934592,
      "usedBytes": 7300000000,
      "usagePct": 85.0,
      "swapUsedPct": 12.0
    },
    "disk": [
      { "mount": "/", "usagePct": 91.2, "freeBytes": 5000000000 }
    ],
    "network": [
      { "iface": "eth0", "rxBytesPerSec": 120000, "txBytesPerSec": 45000,
        "errIn": 0, "errOut": 0 }
    ],
    "processes": {
      "total": 210,
      "zombie": 1,
      "topCpu": [ { "pid": 8123, "name": "xmrig", "cpuPct": 96.0 } ],
      "topMem": [ { "pid": 990,  "name": "java",  "memPct": 22.0 } ]
    }
  },
  "localAlerts": [],
  "inventory": {
    "software": [
      {
        "name": "7-Zip 25.01 (x64)",
        "type": "MSI",
        "vendor": "Igor Pavlov",
        "version": "25.01",
        "guid": "{23170F69-40C1-2603-2501-000001000000}",
        "installedAt": "2026-03-14T00:00:00Z",
        "installPath": "C:\\Program Files\\7-Zip\\",
        "architecture": "x64",
        "sizeBytes": 5242880,
        "status": "installed",
        "source": "registry"
      }
    ]
  }
}
```

---

## 2. Campos, uno a uno

### Nivel raíz

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `agentVersion` | cadena, 1–32 | Sí | Se persiste en `MonitoredAsset.agent_version`. Un binario compilado sin `-ldflags` reporta `0.1.0-dev`. |
| `collectedAt` | ISO-8601 UTC | Sí | Reloj **del agente**, no de fiar. Truncado a microsegundos: el backend rechaza más de 6 decimales. Ventana de cordura en §4. |
| `host` | objeto | Sí | |
| `metrics` | objeto | Sí | |
| `localAlerts` | lista | No | Aceptado y persistido, **pero el backend todavía no lo procesa**, y el agente todavía no lo rellena. Ver F-06 del análisis. |
| `inventory` | objeto o ausente | No | Ausente ≠ vacío: ver §3. |

### `host`

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `hostname` | cadena, 1–255 | Sí | Dato **no confiable**: lo controla quien controla el agente. Escapar al mostrarlo. |
| `os` | cadena, ≤64 | No | `linux`, `windows`, `darwin`. |
| `kernel` | cadena, ≤128 | No | Si falta, el backend conserva el último conocido: es identidad del host. |
| `uptimeSec` | entero ≥0 | No | Estado instantáneo: el backend lo sobreescribe siempre, incluido a nulo. |
| `virtualizationSystem` | cadena | No | `kvm`, `vmware`, `hyperv`, `xen`, `docker`... Ausente si `gopsutil` no detecta ninguno. Ver P29 del proyecto de consumo energético. |
| `virtualizationRole` | cadena | No | `guest` o `host`. Permite distinguir "sin sensores de potencia" de "esto es una máquina virtual, que no los tiene por diseño". El backend todavía no lo interpreta (P29 sin implementar en `EllysiaServer`); el agente ya lo envía. |

### `metrics.cpu` (obligatorio)

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `usagePct` | decimal 0–100 | Sí | Media de todos los núcleos, sobre el intervalo completo entre heartbeats. |
| `loadAvg` | lista de ≤8 decimales | No | Solo en sistemas tipo Unix. En Windows se omite. |
| `ctxSwitches` | entero | No | **Hoy viaja siempre a cero: ningún colector lo rellena.** Pendiente, A-11. |
| `perCorePct` | lista de ≤1024 decimales | No | Un decimal. Mismo cálculo que `usagePct`, por núcleo. |

### `metrics.memory` (obligatorio)

| Campo | Tipo | Obligatorio |
|---|---|---|
| `totalBytes` | entero | No |
| `usedBytes` | entero | No |
| `usagePct` | decimal 0–100 | Sí |
| `swapUsedPct` | decimal 0–100 | No |

### `metrics.disk` — lista, máximo 64

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `mount` | cadena, 1–256 | Sí | Dato no confiable. Se filtran los sistemas de ficheros virtuales. |
| `usagePct` | decimal 0–100 | Sí | |
| `freeBytes` | entero | No | |

### `metrics.network` — lista, máximo 64

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `iface` | cadena, 1–64 | Sí | Se omite `lo`. |
| `rxBytesPerSec` | número | No | **Tasa** por segundo. El agente envía decimal y el backend lo trunca a entero. |
| `txBytesPerSec` | número | No | Igual. |
| `errIn` | entero | No | **Contador acumulado desde el arranque, no una tasa** — a diferencia de los dos campos anteriores. Inconsistencia conocida, A-11. |
| `errOut` | entero | No | Igual. |

El primer heartbeat tras arrancar **omite `network` por completo**: sin muestra
anterior no hay tasa que calcular, y falsear un cero sería peor que no enviar.

### `metrics.processes`

| Campo | Tipo | Notas |
|---|---|---|
| `total` | entero | |
| `zombie` | entero | **Siempre 0 en Windows**: el concepto no existe allí. |
| `topCpu` | lista de ≤20 | El agente envía 5. Vacío en el primer ciclo (sin línea base de CPU). |
| `topMem` | lista de ≤20 | El agente envía 5. |

Cada entrada de `topCpu`/`topMem`:

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `pid` | entero | Sí | |
| `name` | cadena, 1–256 | Sí | Dato no confiable. |
| `cpuPct` | decimal 0–100 | No | Ver el aviso de abajo. Solo en `topCpu`. |
| `memPct` | decimal 0–100 | No | Solo en `topMem`. |

> **Semántica de `cpuPct` — cambió en la v1.1.** Es el porcentaje de la
> capacidad **total del equipo**, no de un núcleo. Un proceso que satura los
> ocho núcleos de una máquina de ocho reporta `100`, no `800`.
>
> Es distinto de lo que muestra `top`, y es deliberado: el rango admitido es
> `[0,100]` y un valor fuera de rango no cuesta ese campo, **cuesta el
> heartbeat entero**. Además, así `cpuPct` y `metrics.cpu.usagePct` están en la
> misma escala y se pueden comparar en el panel. Ver A-01 del análisis.

Ninguna de las dos listas puede ser `null`: una lista vacía se envía como `[]`.
El esquema del backend rechaza `null` en un campo de lista.

### `metrics.power`

> Añadido en la v1.2 (proyecto [Hygeia — Consumo
> energético](https://github.com/orgs/ProjectEllysia/projects/7), fase 0).
> El backend todavía no valida ni persiste este bloque — la regla general de
> §7 aplica: lo desconocido se descarta sin romper nada —, así que el agente
> puede empezar a enviarlo antes de que el otro lado lo entienda.

| Campo | Tipo | Obligatorio | Notas |
|---|---|---|---|
| `watts` | decimal | Sí (si el bloque existe) | Potencia instantánea o su mejor aproximación. `0` es un valor válido y distinto de que el bloque esté ausente. |
| `estimated` | booleano | Sí | Distingue una lectura de sensor (`false`) de una construcción nuestra, por ejemplo un modelo de utilización en Windows (`true`). |
| `source` | cadena | Sí | Cadena libre: `rapl`, `hwmon`, `nvidia`, `amd_gpu`, `psu`, `model`, o combinaciones como `rapl+nvidia`. No es un catálogo cerrado a propósito: una fuente nueva no debería exigir versionar el contrato. |

El bloque entero está **ausente** cuando esta máquina no tiene ninguna fuente
de potencia que reportar — esa es hoy la situación normal, no la excepcional:
la Fase 0 de este proyecto solo fija el contrato y la maquinaria de
recolección; los proveedores reales de Linux y Windows llegan en la fase
siguiente. Un heartbeat sin `power` conserva `cpu`, `memory` y el resto de
métricas con normalidad.

### `inventory.software` — lista, máximo 2000

Todos los campos salvo `name` son opcionales.

| Campo | Tipo | Notas |
|---|---|---|
| `name` | cadena, 1–512 | Obligatorio. |
| `type` | cadena, ≤32 | `MSI`, `EXE`… |
| `vendor` | cadena, ≤256 | |
| `version` | cadena, ≤128 | |
| `guid` | cadena, ≤128 | |
| `installedAt` | cadena, ≤32 | **Cadena, no fecha**: viaja tal cual dentro del JSONB y nunca se computa contra ella. |
| `installPath` | cadena, ≤1024 | |
| `architecture` | cadena, ≤16 | |
| `sizeBytes` | entero ≥0 | |
| `status` | cadena, ≤32 | |
| `source` | cadena, ≤32 | `registry` (Windows), `dpkg`, `rpm`, `flatpak`, `snap`. |

---

## 3. Cadencia del inventario

El inventario **no viaja en cada heartbeat**. Escanearlo es mucho más lento que
recolectar métricas y el dato cambia con poca frecuencia.

- El agente escanea cada `inventoryIntervalSec` (6 horas por defecto) y adjunta
  el resultado **al primer heartbeat siguiente**, una sola vez.
- **Campo ausente** significa "no he escaneado en este heartbeat": el backend
  conserva el inventario anterior.
- Desde `F-05`, el agente **omite el inventario si no ha cambiado** respecto al
  último que el backend aceptó, apoyándose justo en esa regla. Lo reenvía de
  todas formas una vez al día, para que un inventario perdido en el servidor
  acabe reconciliándose sin intervención.
- **Lista vacía** sí es un reemplazo válido: un equipo sin software, o un
  colector que no encuentra ninguna fuente conocida (por ejemplo, una
  distribución que no use dpkg mientras el resto de gestores está pendiente,
  o macOS, que sigue sin implementar — ver F-01).
- **`null` no es válido.** El campo es una lista obligatoria y el backend
  rechaza el heartbeat completo con un 422 si llega nula. Un colector que no
  encuentre nada debe enviar `[]`.
- Nunca es un delta: cada envío es el estado completo y **reemplaza** al
  anterior.
- El agente acota la lista a `inventoryMaxItems` (1500 por defecto) antes de
  enviarla, con holgura deliberada por debajo del tope del backend.

---

## 4. La respuesta

```jsonc
{
  "ok": true,
  "nextIntervalSec": 15,
  "serverTime": "2026-08-13T10:00:01Z"
}
```

`nextIntervalSec` permite al backend reajustar la cadencia de un activo **sin
redesplegar el agente**. El agente lo aplica en caliente, reiniciando su ticker.

---

## 5. Errores, y qué hace el agente con cada uno

| Código | Motivo | Reacción del agente |
|---|---|---|
| `400` | `collectedAt` fuera de la ventana de reloj | **Permanente**: descarta el payload. Repone el inventario que llevara adjunto. |
| `401` | Clave inválida o revocada | Transitorio: al buffer. Ver F-03 — hoy se reintenta indefinidamente. |
| `413` | Cuerpo o descompresión por encima del tope | **Permanente**: descarta. |
| `422` | El cuerpo no cumple el esquema | **Permanente**: descarta. |
| `429` | Por debajo del suelo de cadencia | **Cadencia**: espera lo que indique y reintenta. No es un fallo. |
| `5xx`, red | Backend caído o inalcanzable | Transitorio: reintento con espera exponencial y, si falla, al buffer. |

El `429` trae el tiempo de espera en `details.min_interval_sec`; el agente
respeta antes la cabecera estándar `Retry-After` si viene, porque puede
ponerla un proxy intermedio.

**Permanente** significa que reintentar exactamente el mismo cuerpo nunca va a
funcionar: el dato ya está recolectado y no puede cambiar. Guardarlo en el
buffer solo garantizaría que ocupe un hueco para siempre.

---

## 6. Límites del backend

Configurables en caliente vía `PUT /system`, bloque
`features.hygeia.limits`. Valores por defecto:

| Límite | Valor | Qué acota |
|---|---|---|
| `maxBodyBytes` | 1 MiB | Cuerpo comprimido. |
| `maxDecompressedBytes` | 4 MiB | Descompresión, con corte duro (anti *gzip bomb*). |
| `maxProcesses` | 20 | `topCpu` y `topMem`. |
| `maxDiskMounts` | 64 | `disk`. |
| `maxNetInterfaces` | 64 | `network`. |
| `maxInventoryItems` | 2000 | `inventory.software`. |
| `minIntervalSec` | 5 s | Suelo entre heartbeats de una misma clave. |
| `clockSkewSec` | 300 s | Cuánto puede **adelantarse** `collectedAt`. |
| `maxBackfillSec` | 86400 s | Cuánto puede **atrasarse** `collectedAt`. |
| `maxAssetsPerUser` | 500 | Activos por usuario. |

La ventana de reloj es **asimétrica** a propósito. Un heartbeat adelantado no
tiene explicación legítima —ningún retardo de red adelanta un reloj— y conviene
rechazarlo pronto. Uno atrasado sí la tiene, y es la razón de ser del buffer en
disco: el agente drenando lo que guardó mientras el backend estaba caído. Con
una ventana simétrica corta, ese buffer era decorativo. Ver A-02 del análisis.

---

## 7. Reglas del contrato

- El backend **valida y descarta lo desconocido**. Enviar campos de más no
  rompe nada, pero tampoco se persiste.
- La identidad del activo la determina **la clave**, nunca un identificador del
  cuerpo.
- Se evoluciona **por extensión**: campos nuevos opcionales, nunca cambios que
  rompan. `agentVersion` permite al backend saber con quién habla.
- Todo lo que trae el agente es **dato no confiable**: hostnames, nombres de
  proceso y puntos de montaje los controla quien controla la máquina. Escapar
  al mostrarlos y tratarlos como valores al registrarlos.

---

## 8. Historial de versiones

| Versión | Cambio |
|---|---|
| **1.2** | `metrics.power` (`watts`, `estimated`, `source`), ausente sin fuente de potencia. `host.virtualizationSystem` y `host.virtualizationRole`, para que el backend distinga "sin sensores" de "es una máquina virtual" (P29). Fase 0 del proyecto de consumo energético: todavía no hay ninguna implementación real por sistema operativo. |
| **1.1** | `cpuPct` de proceso pasa a ser porcentaje de la capacidad total del equipo, normalizado por número de núcleos (A-01). Ventana de reloj asimétrica, con `maxBackfillSec` nuevo (A-02). El `429` expone `min_interval_sec` (A-03). Documentado `inventory`, que existía sin estar en el contrato escrito. |
| **1.0** | Contrato inicial: métricas de CPU, memoria, disco, red y procesos, más `nextIntervalSec` en la respuesta. |
