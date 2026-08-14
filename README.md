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

## Instalar desde una release

Cada etiqueta `v*` publica en GitHub binarios para `linux/amd64`, `linux/arm64`,
`windows/amd64`, `darwin/amd64` y `darwin/arm64`, paquetes `.deb` y `.rpm`, el instalador de
Windows y un `checksums.txt`.

| Fichero | Qué lleva dentro |
|---|---|
| `hygeia-agent-setup-<version>.exe` | **La forma normal de instalar en Windows.** Instalador de doble clic: los dos binarios, el `serverUrl` ya relleno, el servicio registrado y el tray en el arranque de sesión. |
| `hygeia_<version>_windows_amd64.zip` | Los mismos dos ejecutables sueltos, para despliegue automatizado sin interfaz. |
| `hygeia_<version>_linux_*.tar.gz` · `hygeia_<version>_darwin_*.tar.gz` | Solo `hygeia-agent`: fuera de Windows no se distribuye el tray. |
| `hygeia-agent_<version>_linux_*.deb` · `.rpm` | Paquetes nativos, con el servicio registrado al instalar. |

**Los binarios todavía no están firmados.** Comprueba la suma antes de instalar, y cuenta con el
aviso de SmartScreen en Windows:

```bash
sha256sum -c checksums.txt --ignore-missing
```

En Debian, Ubuntu y derivadas:

```bash
sudo apt install ./hygeia-agent_<version>_linux_amd64.deb
```

En Fedora, RHEL y derivadas:

```bash
sudo dnf install ./hygeia-agent_<version>_linux_amd64.rpm
```

El paquete deja el binario en `/usr/bin`, la configuración en `/etc/hygeia/config.toml` (con
permisos `0600`, porque ahí acaba la clave del agente) y registra el servicio. Después queda
poner la URL del servidor, arrancar y dar de alta:

```bash
sudo systemctl start hygeia-agent
echo "$CLAVE_DE_AGENTE" | sudo hygeia-agent enroll
```

Una actualización conserva `config.toml` —y con él la clave— y reinicia el servicio solo si
estaba en marcha.

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

## Dar de alta el agente

Un agente recién instalado no tiene clave y no reporta nada hasta que se le da una. Se puede
hacer desde el icono de bandeja, o desde la línea de órdenes — que es lo único disponible en un
servidor sin escritorio:

```bash
echo "$CLAVE_DE_AGENTE" | hygeia-agent enroll
```

Se admite también `hygeia-agent enroll <clave>`, pero **la clave pasada como argumento queda
visible** para cualquier usuario de la máquina con `ps`, y se queda en el historial del
intérprete. Por eso la forma recomendada, y la que conviene usar en un `cloud-init` o un
playbook de Ansible, es la entrada estándar.

Para retirarle la clave (deja de reportar hasta que se le dé de alta otra vez):

```bash
hygeia-agent reset
```

Pregunta antes de borrar. Sin terminal donde confirmar, exige `--yes` explícitamente en vez de
darlo por hecho.

## Diagnosticar

Dos preguntas distintas, dos órdenes distintas:

```bash
hygeia-agent status   # ¿está vivo el proceso? (se lo pregunta al gestor de servicios)
hygeia-agent info     # ¿está reportando? (se lo pregunta al propio agente)
```

Un agente puede estar perfectamente en ejecución y llevar horas sin poder entregar un
heartbeat; `info` enseña el estado de conexión, cuántos payloads hay en el buffer, cuándo fue
el último envío y el último error.

Y cuando la respuesta es "no está reportando" y hace falta saber **por qué**:

```bash
hygeia-agent doctor
```

Comprueba, en el orden en que se descartan las causas: el fichero de configuración y sus
permisos, la URL, el formato de la clave, el proxy y la CA propia, la resolución DNS, la
conectividad TCP, el certificado TLS y su caducidad, si el servidor acepta la clave, la
desviación del reloj, y el estado del servicio. Cada fallo lleva debajo qué hacer, y el código de
salida es distinto de cero si algo va mal, para poder usarlo desde un script de despliegue.

No imprime nunca el secreto de la clave ni la contraseña del proxy: esta salida es lo primero
que se pega en un ticket de soporte.

Para las interioridades del proceso, sin buscar el fichero de log:

```bash
hygeia-agent debug
```

## Redes corporativas

Si la red obliga a pasar por un proxy, o inspecciona el tráfico TLS con una autoridad de
certificación propia, hay dos campos en `config.toml`:

```toml
proxyUrl = "http://usuario:clave@proxy.empresa.local:3128"
caFile   = "C:/ProgramData/Hygeia/empresa-ca.pem"
```

Sin `caFile`, en una red con inspección TLS el agente **no puede conectar en absoluto**: el
certificado que ve está emitido por la autoridad interna, y rechazar certificados inválidos no
es negociable en un producto de seguridad. El certificado indicado se **suma** al almacén del
sistema, nunca lo sustituye, para que el agente siga funcionando fuera de la oficina.

`proxyUrl` hace falta aunque estén definidas `HTTP_PROXY` y `HTTPS_PROXY`: un servicio de Windows
corriendo como `LocalSystem` no hereda las variables de entorno del usuario.

`hygeia-agent doctor` comprueba los dos con la misma configuración que usa el servicio.

## Si Ellysia rota la clave

Rotar una clave desde Ellysia invalida la anterior de inmediato. El agente lo detecta, deja de
enviar —no llena el buffer con lo que ya no se puede entregar— y el icono de bandeja pasa a rojo
con el texto "clave rechazada". La nueva se aplica directamente, sin necesidad de reestablecer
nada antes:

```bash
echo "$CLAVE_NUEVA" | hygeia-agent enroll
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
