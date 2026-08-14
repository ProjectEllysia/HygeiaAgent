# Ellysia — Hygeia (agente)

> Agente ligero de monitorización de activos para **Ellysia**. Se instala en cada host,
> recolecta métricas de hardware (CPU, memoria, disco, red, procesos) cada N segundos y las
> empuja al backend de Ellysia (`POST /hygeia/ingest`).
>
> Hygeia (Ὑγίεια) es la diosa griega de la salud: este agente toma los **signos vitales**
> del activo y los reporta; quien decide si algo está "enfermo" es el backend.

Dos binarios:

| Software | Proposito |
|---|---|
| `hygeia-agent` | El servicio: recolecta y envía. Es el único imprescindible. |
| `hygeia-tray` | Companion de bandeja, opcional: muestra el estado y permite dar de alta el activo sin editar ficheros a mano. Solo para estaciones de trabajo. |

---

## Documentación

| Documento | Para qué |
|---|---|
| Este README | Compilar, instalar y usar el agente. |
| [`docs/DISENO.md`](docs/DISENO.md) | Por qué el agente es como es: filosofía, arquitectura, decisiones. |
| [`docs/CONTRATO-INGESTA.md`](docs/CONTRATO-INGESTA.md) | **La costura con el backend.** Fuente única: los dos repositorios enlazan aquí en vez de copiarlo. |
| [`docs/ANALISIS-INGENIERIA.md`](docs/ANALISIS-INGENIERIA.md) | Auditoría técnica y plan de trabajo por fases. |

---

## Desarrollo (primer plano)

```bash
cp config.example.toml config.toml
```

Rellena `serverUrl` y arranca:

```bash
HYGEIA_CONFIG=$PWD/config.toml go run ./cmd/hygeia-agent
```

Sin `agentKey` el agente **no falla**: arranca en estado "sin configurar" y espera a que le
llegue la clave.

## Compilar

```bash
go build -ldflags "-X github.com/ProjectEllysia/Ellysia-Hygeia/internal/version.Version=1.0.4" ./cmd/hygeia-agent
```

El tray, en Windows, sin ventana de consola:

```bash
go build -ldflags "-H=windowsgui" ./cmd/hygeia-tray
```

Sin `-ldflags`, el binario reporta la versión `0.1.0-dev` al backend.

| | Windows | Linux | macOS |
|---|---|---|---|
| `hygeia-agent` | ✅ | ✅ | ✅ (cross-compila) |
| `hygeia-tray` | ✅ | ✅ | requiere cgo y toolchain nativo |

En Windows hay además un instalador de doble clic, que empaqueta ambos binarios con el
`serverUrl` ya relleno:

```powershell
.\installer\build-installer.ps1
```

## Instalar como servicio

Se registra en el gestor de servicios del sistema operativo (systemd, servicio de Windows o
launchd) vía `kardianos/service`. Requiere privilegios de administrador o root:

```bash
hygeia-agent install
```

```bash
hygeia-agent start
hygeia-agent status
hygeia-agent stop
hygeia-agent uninstall
```

La config vive en el directorio de estado del servicio, no junto al binario: el servicio corre
con un working directory que no controlamos (en Windows, System32).

| SO | Directorio de estado |
|---|---|
| Windows | `C:\ProgramData\Hygeia\` |
| Linux | `/etc/hygeia/` |
| macOS | `/Library/Application Support/Hygeia/` |

Ahí van `config.toml`, `buffer.jsonl` y `hygeia-agent.log`. En modo servicio los logs van al
fichero, rotado a 5 MB y conservando un fichero anterior; en primer plano, a stderr.

Para diagnosticar un servicio ya en marcha, sin buscar el fichero de log:

```bash
hygeia-agent debug
```

## Instalar el icono de bandeja

Opcional y **sin privilegios**: se registra en el arranque por usuario, no en el del sistema.

```bash
hygeia-tray
```

```bash
hygeia-tray enable-autostart
hygeia-tray status
hygeia-tray disable-autostart
```

Un servidor sin escritorio corre `hygeia-agent` solo, sin tray, exactamente igual.

## Configuración

Fichero TOML con override por variables de entorno. Todas las opciones, con sus valores por
defecto, sus rangos y por qué existen, están documentadas en
[`config.example.toml`](config.example.toml).

```toml
serverUrl   = "https://ellysia.tu-dominio/hygeia"
agentKey    = "..."          # opcional: puede llegar por el tray
intervalSec = 15
collectors  = ["cpu", "memory", "disk", "network", "processes"]
```

## Estructura del repositorio

```
cmd/hygeia-agent/   el servicio
cmd/hygeia-tray/    el companion de bandeja
internal/           todo el código del agente (ver docs/DISENO.md §4)
docs/               diseño, contrato de ingesta y análisis técnico
installer/          empaquetado para Windows
resources/          arte de marca
```
