# Ellysia — Hygeia (agente)

> Agente ligero de monitorización de activos para **Ellysia**. Se instala en cada host,
> recolecta métricas de hardware (CPU, memoria, disco, red, procesos) cada N segundos y las
> empuja al backend de Ellysia (`POST /hygeia/ingest`).
>
> Hygeia (Ὑγίεια) es la diosa griega de la salud: este agente toma los **signos vitales**
> del activo y los reporta; quien decide si algo está "enfermo" es el backend.

El proyecto produce dos binarios. `hygeia-agent` es el servicio que recolecta las métricas y
las envía: es el único imprescindible, y el que expone todos los subcomandos que se documentan
en este README. `hygeia-tray` es un companion opcional que vive en la bandeja del sistema,
pensado para estaciones de trabajo con escritorio: enseña el estado del agente con un icono y
permite dar de alta el activo sin tener que usar la línea de órdenes. Un servidor sin
escritorio no lo necesita para nada: `hygeia-agent` funciona exactamente igual sin él.

---

## Documentación

Este README cubre cómo compilar, instalar y usar el agente en el día a día. Para el resto de
preguntas hay documentos dedicados:

| Documento | Para qué sirve |
|---|---|
| [`docs/DISENO.md`](docs/DISENO.md) | Explica por qué el agente está construido como está: la filosofía detrás de las decisiones de arquitectura, no solo el qué sino el porqué. |
| [`docs/CONTRATO-INGESTA.md`](docs/CONTRATO-INGESTA.md) | Describe la costura exacta con el backend: la forma del payload, los límites y las reglas de validación. Es la fuente única de esa información — los dos repositorios enlazan aquí en vez de mantener una copia cada uno, que acabaría divergiendo. |
| [`docs/ANALISIS-INGENIERIA.md`](docs/ANALISIS-INGENIERIA.md) | Auditoría técnica del proyecto y el plan de trabajo dividido por fases, con el estado de cada punto. |

---

## Desarrollo en primer plano

Para trabajar en el agente sin instalarlo como servicio, primero se copia la plantilla de
configuración:

```bash
cp config.example.toml config.toml
```

Se rellena el campo `serverUrl` con la dirección del backend contra el que se va a probar, y se
arranca apuntando explícitamente a ese fichero:

```bash
HYGEIA_CONFIG=$PWD/config.toml go run ./cmd/hygeia-agent
```

No hace falta que el fichero tenga ya una `agentKey`: si falta, el agente arranca igualmente en
el estado "sin configurar" y se queda esperando a que se la entreguen, en vez de fallar. Esto
permite desplegar el binario y darlo de alta después, en un segundo paso.

## Compilar

Para compilar el binario del agente con la versión correcta incrustada, hay que pasarla por
`-ldflags`:

```bash
go build -ldflags "-X github.com/ProjectEllysia/Ellysia-Hygeia/internal/version.Version=1.0.5" ./cmd/hygeia-agent
```

Si se omite `-ldflags`, el binario resultante reporta la versión `0.1.0-dev` al backend. Esto
es intencionado: sirve para distinguir de un vistazo, en el panel de Ellysia, un activo que
corre un binario oficial de uno que corre algo compilado a mano por un desarrollador.

El companion de bandeja se compila igual, con una bandera adicional en Windows para que no
abra una ventana de consola detrás del icono:

```bash
go build -ldflags "-H=windowsgui" ./cmd/hygeia-tray
```

No todas las combinaciones de binario y sistema operativo están disponibles de la misma forma:

| | Windows | Linux | macOS |
|---|---|---|---|
| `hygeia-agent` | Compila de forma nativa | Compila de forma nativa | Compila por cross-compilación desde cualquier otro sistema |
| `hygeia-tray` | Compila de forma nativa | Compila de forma nativa | Necesita cgo y un toolchain de C nativo del propio macOS; no se puede cruzar desde Linux o Windows |

## Compilar los paquetes de distribución

Generar los paquetes que de verdad se instalan en una máquina —instaladores, `.deb`, `.rpm`,
archivos comprimidos— usa dos herramientas distintas, cada una responsable de una parte. Ninguna
de las dos escribe nada dentro del repositorio: tanto `dist/` como `installer/dist/` están
excluidas del control de versiones, así que se pueden borrar y regenerar sin ningún riesgo.

### Todas las plataformas a la vez

```bash
goreleaser release --snapshot --clean --skip=publish
```

Tarda unos siete segundos. Deja en `dist/` los binarios sueltos, los archivos `.tar.gz` y `.zip`
ya empaquetados, los paquetes `.deb` y `.rpm` para Linux, y un `checksums.txt` con la suma de
verificación de cada fichero. Esto es exactamente lo mismo que ejecutará la integración continua
al publicar una versión real, así que sirve para comprobar en local que una release va a salir
bien antes de crear la etiqueta que la dispara.

### El instalador de Windows

```powershell
.\installer\build-installer.ps1
```

Este script compila los dos ejecutables, genera el guion de Inno Setup a partir de una plantilla
y produce `installer/dist/hygeia-agent-setup-<version>.exe`, el instalador de doble clic. Para
ejecutarlo hacen falta dos cosas:

- Un fichero `config.toml` en la raíz del repositorio, del que el script toma el `serverUrl`
  que queda empotrado dentro del instalador. Este fichero no está en el repositorio porque
  contiene datos concretos del despliegue; se crea con `cp config.example.toml config.toml` y
  rellenando la URL real.
- Inno Setup instalado en la máquina. Si el script no lo encuentra, intenta instalarlo
  automáticamente con WinGet; si tampoco puede, deja el fichero `.iss` generado y listo para
  compilarlo a mano más tarde.

La versión que se incrusta en el instalador sale de `VERSION.txt` por defecto, aunque se puede
forzar un valor puntual con `-Version 1.2.3` para hacer una prueba sin tocar ese fichero.

Al generar la copia del `config.toml` que viaja dentro del instalador, el script quita
deliberadamente dos campos. El primero es `agentKey`: si el `config.toml` local del
desarrollador tuviera una clave real, distribuirla dentro del instalador la filtraría a todos
los clientes que lo usaran, y todos ellos acabarían colisionando sobre el mismo activo en
Ellysia. El segundo es `bufferPath`: si en el fichero local es una ruta relativa (algo normal
cuando se ejecuta con `go run` desde el propio repositorio), esa ruta relativa se resolvería
contra el directorio de trabajo del servicio ya instalado y no contra la carpeta del repositorio,
rompiendo la ubicación del buffer.

### Por qué existen dos herramientas en vez de una sola

Puede parecer redundante compilar los binarios de Windows dos veces —una vez dentro de
goreleaser y otra dentro del script del instalador— pero el coste real es de segundos de CPU, y
no compensa forzar una única herramienta. El motivo es que el instalador hace varias cosas que
goreleaser no hace y que no tendría sentido que hiciera: para el servicio existente y cierra el
proceso del tray antes de sobrescribir sus ejecutables (si siguieran corriendo, estarían
bloqueados y la copia de ficheros fallaría), registra el arranque automático del tray para la
sesión del usuario que está instalando en vez de para la cuenta con privilegios elevados del
propio instalador, y en una actualización nunca sobrescribe el `config.toml` que ya tiene el
cliente, para no perder la clave con la que ese activo está dado de alta.

## Publicar una versión

Publicar una versión nueva consiste en dos pasos: preparar el número de versión y, después,
etiquetar el commit correspondiente para disparar la publicación automática.

```bash
# 1. VERSION.txt y la etiqueta que se cree después TIENEN que coincidir:
#    la integración continua comprueba esto explícitamente y aborta si no
#    coinciden, para que nunca se publique un instalador con una versión
#    distinta de la que reportarán los agentes que instale.
echo 1.0.6 > VERSION.txt
git commit -am "chore: versión 1.0.6"

# 2. Etiquetar el commit y empujar la etiqueta al remoto. Empujar la
#    etiqueta es lo que dispara el flujo de publicación; crearla en local
#    con "git tag" no activa nada por sí solo.
git tag v1.0.6
git push origin main --tags
```

Una vez empujada la etiqueta, el flujo de integración continua compila las cinco plataformas,
genera los paquetes y las sumas de verificación, ejecuta `build-installer.ps1` en un runner de
Windows, adjunta el `.exe` resultante a la release, y crea esa release en GitHub **en estado de
borrador**. Queda revisarla manualmente —comprobar que están todos los ficheros esperados y que
las notas tienen sentido— y publicarla desde la interfaz de GitHub cuando se esté conforme.

Para que el paso del instalador de Windows pueda ejecutarse dentro de la integración continua
hace falta definir de antemano la variable de repositorio `HYGEIA_SERVER_URL`, en
Settings → Secrets and variables → Actions → Variables. Si esa variable no existe, el paso se
salta con un aviso en los logs, en vez de publicar por error un instalador que apuntaría al
marcador de posición del fichero de ejemplo.

## Instalar desde una release

Cada etiqueta con el formato `v*` que se publica genera automáticamente en GitHub binarios para
`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64` y `darwin/arm64`, paquetes `.deb`
y `.rpm` para Linux, el instalador de Windows, y un fichero `checksums.txt` con las sumas de
verificación de todo lo anterior.

| Fichero | Qué contiene y cuándo usarlo |
|---|---|
| `hygeia-agent-setup-<version>.exe` | La forma normal de instalar en Windows: un instalador de doble clic que incluye los dos binarios, la URL del servidor ya rellena, el registro del servicio y el arranque automático del tray. |
| `hygeia_<version>_windows_amd64.zip` | Los mismos dos ejecutables sueltos, sin instalador. Pensado para despliegue automatizado en máquinas donde no se puede o no se quiere ejecutar una interfaz gráfica. |
| `hygeia_<version>_linux_*.tar.gz` y `hygeia_<version>_darwin_*.tar.gz` | Solo el binario `hygeia-agent`: fuera de Windows el companion de bandeja no se distribuye, porque esas plataformas suelen usarse en servidores sin escritorio. |
| `hygeia-agent_<version>_linux_*.deb` y `.rpm` | Los paquetes nativos de cada familia de distribuciones, que registran el servicio automáticamente al instalarse. |

Los binarios todavía no están firmados digitalmente. Antes de instalar cualquiera de ellos
conviene comprobar su integridad contra las sumas publicadas:

```bash
sha256sum -c checksums.txt --ignore-missing
```

En Windows, al ejecutar el instalador sin firmar, hay que contar con el aviso de SmartScreen
("Windows protegió tu PC"); se continúa desde "Más información → Ejecutar de todos modos".

En Debian, Ubuntu y derivadas se instala así:

```bash
sudo apt install ./hygeia-agent_<version>_linux_amd64.deb
```

Y en Fedora, RHEL y derivadas:

```bash
sudo dnf install ./hygeia-agent_<version>_linux_amd64.rpm
```

El paquete deja el binario en `/usr/bin`, escribe la configuración en `/etc/hygeia/config.toml`
con permisos `0600` (porque ese fichero acaba guardando la clave del agente, y no debe poder
leerlo cualquier otro usuario de la máquina), y registra el servicio del sistema. Después de
instalar quedan dos pasos: poner la URL real del servidor en ese fichero, y arrancar el servicio
y darlo de alta:

```bash
sudo systemctl start hygeia-agent
echo "$CLAVE_DE_AGENTE" | sudo hygeia-agent enroll
```

Cuando se instala una versión más nueva encima de una ya instalada, el paquete conserva el
`config.toml` existente —y con él la clave con la que ese activo ya estaba dado de alta— y
reinicia el servicio únicamente si ya estaba en marcha; si alguien lo había parado a propósito,
la actualización no lo vuelve a arrancar por su cuenta.

## Instalar como servicio del sistema operativo

Independientemente de cómo se haya obtenido el binario, se registra como servicio del sistema
operativo (systemd en Linux, el gestor de servicios de Windows, o launchd en macOS) a través de
la librería `kardianos/service`. Esta operación requiere privilegios de administrador en
Windows o de root en Linux y macOS:

```bash
hygeia-agent install
```

A partir de ahí, el servicio se gestiona con las órdenes habituales:

```bash
hygeia-agent start
hygeia-agent status
hygeia-agent stop
hygeia-agent uninstall
```

La configuración vive en un directorio de estado propio del servicio, y no junto al binario:
esto es necesario porque, en modo servicio, el proceso arranca con un directorio de trabajo que
no se controla (en Windows, por ejemplo, suele ser `System32`), así que no se puede asumir que
el binario y sus datos compartan ubicación.

| Sistema operativo | Directorio de estado |
|---|---|
| Windows | `C:\ProgramData\Hygeia\` |
| Linux | `/etc/hygeia/` |
| macOS | `/Library/Application Support/Hygeia/` |

En ese directorio conviven tres ficheros: `config.toml`, `buffer.jsonl` (el buffer de
heartbeats pendientes de entregar cuando el backend no responde) y `hygeia-agent.log`. Cuando el
agente corre como servicio, sus logs van a ese fichero, que se rota automáticamente al alcanzar
5 MB conservando una única copia anterior; cuando corre en primer plano en una terminal, los
logs van directamente a la salida de error estándar.

## Comandos de `hygeia-agent`

El binario del servicio se controla por completo desde la línea de órdenes. Ejecutado sin
argumentos, corre el agente en primer plano; con un subcomando, ejecuta esa acción concreta y
termina.

| Comando | Qué hace |
|---|---|
| `hygeia-agent` | Ejecuta el agente en primer plano, sin registrarse como servicio. Útil para desarrollo y para depurar de forma interactiva. |
| `hygeia-agent install` | Registra el binario como servicio del sistema operativo. Requiere privilegios de administrador o root. |
| `hygeia-agent uninstall` | Elimina el registro del servicio del sistema operativo. |
| `hygeia-agent start` | Arranca el servicio ya registrado. |
| `hygeia-agent stop` | Detiene el servicio en marcha. |
| `hygeia-agent restart` | Detiene y vuelve a arrancar el servicio. |
| `hygeia-agent status` | Pregunta al gestor de servicios del sistema operativo si el proceso está vivo. No dice nada sobre si el agente está reportando de verdad al backend. |
| `hygeia-agent enroll [clave]` | Da de alta el agente entregándole la clave al servicio en marcha. Si no se pasa la clave como argumento, se lee de la entrada estándar, que es la forma recomendada — ver la sección siguiente. |
| `hygeia-agent reset [--yes]` | Borra la clave local; el agente deja de reportar hasta que se le dé de alta de nuevo. Pide confirmación por terminal salvo que se pase `--yes`. |
| `hygeia-agent info` | Pregunta al propio agente por su estado real: si está conectado, cuántos payloads tiene pendientes en el buffer, y cuándo fue el último envío correcto. |
| `hygeia-agent doctor` | Ejecuta una batería de comprobaciones para diagnosticar por qué un agente no está reportando — ver la sección "Diagnosticar" más abajo. |
| `hygeia-agent debug` | Muestra el estado interno del proceso en marcha: número de goroutines, memoria en uso y las líneas de log más recientes. |
| `hygeia-agent version` (o `-v`, `--version`) | Imprime la versión del binario. |
| `hygeia-agent help` (o `-h`, `--help`) | Imprime la ayuda con la lista de subcomandos disponibles. |

## Dar de alta el agente

Un agente recién instalado no tiene clave todavía, y no reporta absolutamente nada hasta que se
le entrega una. Esto se puede hacer desde el icono de bandeja si la máquina lo tiene, o desde la
línea de órdenes, que es la única opción disponible en un servidor sin escritorio:

```bash
echo "$CLAVE_DE_AGENTE" | hygeia-agent enroll
```

También se admite pasar la clave directamente como argumento, con `hygeia-agent enroll <clave>`,
pero esa forma tiene un problema de seguridad real: una clave pasada como argumento de línea de
órdenes queda visible para cualquier otro usuario de la misma máquina que ejecute `ps`, y además
se guarda en el historial del intérprete de comandos. Por eso la forma recomendada —y la que
conviene usar siempre en un script de aprovisionamiento como un `cloud-init` o un playbook de
Ansible— es la entrada estándar, tal como se muestra arriba.

Para retirarle la clave a un agente ya dado de alta (dejará de reportar hasta que se le entregue
una nueva), se usa:

```bash
hygeia-agent reset
```

Por defecto, este comando pregunta por confirmación antes de borrar nada. Si se ejecuta desde un
contexto sin terminal interactivo (por ejemplo, dentro de un script), exige que se le pase el
argumento `--yes` de forma explícita en lugar de asumir una respuesta afirmativa por defecto.

## Diagnosticar

Existen dos preguntas distintas sobre el estado de un agente, y cada una tiene su propio
comando: si el proceso está vivo, y si está reportando de verdad. Confundirlas es fácil, porque
un agente puede estar perfectamente en ejecución y llevar horas sin conseguir entregar un solo
heartbeat.

```bash
hygeia-agent status   # ¿está vivo el proceso? Se lo pregunta al gestor de servicios del SO.
hygeia-agent info     # ¿está reportando? Se lo pregunta al propio agente.
```

`info` muestra el estado de conexión actual, cuántos payloads hay acumulados en el buffer a la
espera de poder entregarse, cuándo fue el último envío que tuvo éxito, y el último error
registrado, si lo hay.

Cuando la respuesta a esa pregunta es que no está reportando, y hace falta averiguar por qué,
existe un tercer comando pensado exactamente para eso:

```bash
hygeia-agent doctor
```

Este comando recorre, en el mismo orden en que se irían descartando las causas manualmente: la
existencia y los permisos del fichero de configuración, la validez de la URL del servidor, el
formato de la clave de agente, la configuración de proxy y de autoridad de certificación propia
si las hubiera, la resolución del nombre de dominio por DNS, la conectividad TCP contra el
servidor, la validez y la fecha de caducidad del certificado TLS, si el servidor acepta la clave
que se le está presentando, la desviación entre el reloj local y el del servidor, y finalmente
el estado del servicio en marcha y su buffer. Cada comprobación que falla imprime debajo una
explicación de qué hacer para solucionarla, y el comando termina con un código de salida
distinto de cero si algo ha fallado, precisamente para que se pueda invocar desde un script de
despliegue automatizado y detectar el fallo sin tener que leer la salida.

Por diseño, esta salida nunca imprime el secreto de la clave de agente ni la contraseña de un
proxy configurado, porque es habitual que el resultado de este comando se copie y se pegue
directamente en un ticket de soporte o en una conversación de chat.

Para ver las interioridades del propio proceso en marcha, sin necesidad de localizar el fichero
de log en disco, está disponible:

```bash
hygeia-agent debug
```

## Redes corporativas

Cuando la red en la que se despliega el agente obliga a pasar el tráfico por un proxy, o
inspecciona las conexiones TLS con una autoridad de certificación propia de la empresa, existen
dos campos en `config.toml` pensados para ese escenario:

```toml
proxyUrl = "http://usuario:clave@proxy.empresa.local:3128"
caFile   = "C:/ProgramData/Hygeia/empresa-ca.pem"
```

Si la red inspecciona TLS y no se configura `caFile`, el agente no puede conectar en absoluto:
el certificado que presenta el servidor está firmado por la autoridad interna de la empresa, y
el agente rechaza certificados que no puede validar, porque eso no es negociable en un producto
de seguridad. El certificado que se indique en `caFile` se **añade** al almacén de certificados
del propio sistema operativo, nunca lo sustituye, precisamente para que el mismo agente siga
funcionando con normalidad cuando el equipo se conecte desde fuera de esa red corporativa.

El campo `proxyUrl` hace falta incluso en máquinas donde ya están definidas las variables de
entorno `HTTP_PROXY` y `HTTPS_PROXY`, porque un servicio de Windows que corre bajo la cuenta
`LocalSystem` no hereda las variables de entorno de ningún usuario que haya iniciado sesión.

El comando `hygeia-agent doctor` comprueba estos dos campos usando exactamente la misma
configuración de red que emplea el propio servicio, así que un resultado correcto en `doctor`
garantiza que el servicio va a poder conectar igual.

## Si Ellysia rota la clave del agente

Rotar la clave de un activo desde Ellysia invalida la clave anterior de forma inmediata. El
agente detecta esa situación por su cuenta: deja de intentar enviar heartbeats con la clave
rechazada —y, en particular, no sigue acumulando payloads en el buffer para una clave que ya se
sabe que no va a funcionar—, y el icono de bandeja, si lo hay, pasa a mostrarse en rojo con el
texto "clave rechazada". La clave nueva se puede aplicar directamente, sin ningún paso
intermedio de reestablecer la configuración:

```bash
echo "$CLAVE_NUEVA" | hygeia-agent enroll
```

## Instalar el icono de bandeja

El companion de bandeja es opcional y no necesita privilegios de administrador: se registra en
el arranque de la sesión del usuario actual, no en el arranque del propio sistema operativo.

```bash
hygeia-tray
```

Su arranque automático se gestiona con sus propios subcomandos:

```bash
hygeia-tray enable-autostart
hygeia-tray status
hygeia-tray disable-autostart
```

Un servidor sin entorno de escritorio simplemente corre `hygeia-agent` en solitario, sin
instalar nunca el tray, y funciona exactamente igual: ninguna funcionalidad del agente depende
de que el companion de bandeja esté presente.

## Configuración

La configuración vive en un fichero TOML, con la posibilidad de sobrescribir cualquier campo
mediante variables de entorno. Todas las opciones disponibles, junto con sus valores por
defecto, los rangos de valores aceptados, y una explicación de por qué existe cada una, están
documentadas directamente en [`config.example.toml`](config.example.toml), que conviene leer
antes de desplegar en producción.

```toml
serverUrl   = "https://ellysia.tu-dominio/hygeia"
agentKey    = "..."          # Opcional: también puede llegar más tarde a través del tray o de `enroll`.
intervalSec = 15
collectors  = ["cpu", "memory", "disk", "network", "processes"]
```

## Estructura del repositorio

```
cmd/hygeia-agent/   El código del binario del servicio.
cmd/hygeia-tray/    El código del companion de bandeja.
internal/           Todo el código compartido del agente (ver docs/DISENO.md §4 para el desglose por paquete).
docs/               Documentación de diseño, el contrato de ingesta con el backend, y el análisis técnico.
installer/          Script y plantilla para generar el instalador de Windows.
resources/          Arte de marca (iconos, logotipos) usado por el tray y por el instalador.
```
