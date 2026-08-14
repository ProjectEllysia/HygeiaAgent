# Registro de cambios

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/).
Las versiones publicadas se anotan en [`VERSION.txt`](VERSION.txt).

Los identificadores `A-nn`, `O-nn` y `F-nn` remiten a
[`docs/ANALISIS-INGENIERIA.md`](docs/ANALISIS-INGENIERIA.md).

## [Sin publicar]

### Añadido

- **Alta y diagnóstico por línea de órdenes** (`F-02`): `hygeia-agent enroll`,
  `reset` e `info`. Hasta ahora, dar de alta un servidor sin escritorio
  obligaba a editar `config.toml` a mano con privilegios y reiniciar el
  servicio. La clave se lee de la entrada estándar
  (`echo "$CLAVE" | hygeia-agent enroll`) porque un argumento queda visible en
  `ps`. `info` responde a una pregunta distinta que `status`: si el agente
  está *reportando*, no si el proceso está *vivo*.
- **Inventario de software en Linux** (`F-01`). El agente lee
  `/var/lib/dpkg/status` en Debian, Ubuntu y derivadas, y consulta RPM,
  Flatpak y Snap donde estén. Hasta ahora el colector de Linux devolvía una
  lista vacía, así que un servidor con el agente instalado aparecía en
  Ellysia como una máquina sin ningún software y su informe de inventario
  salía en blanco. Queda pendiente macOS.
- **El inventario ya no se reenvía si no ha cambiado** (`F-05`). Antes viajaba
  la lista completa cada seis horas aunque no hubiera cambiado nada: en un
  equipo con dos mil aplicaciones, varios cientos de kilobytes cuatro veces al
  día. Se envía cuando cambia, y una vez al día en cualquier caso para
  reconciliar. No requiere cambios en el servidor.

### Corregido

- **`enroll`, `help` y `version` fallaban si no se podía leer la
  configuración** (`F-02`). El binario cargaba `config.toml` antes de mirar qué
  subcomando se había pedido, así que dar de alta un agente recién instalado
  —que puede no tener configuración todavía, o cuyo fichero exige privilegios—
  era imposible. Se carga ahora solo cuando el subcomando la necesita.
- **Un inventario vacío tiraba el heartbeat entero** (`F-01`). Un escaneo sin
  resultados viajaba como `"software": null`, y el backend declara ese campo
  obligatorio y no nulo: respondía 422 y el agente descartaba el heartbeat
  completo. Afectaba a todos los agentes de Linux y macOS, que perdían un
  heartbeat cada seis horas desde que existe el bucle de escaneo.
- **Las versiones de los paquetes de Linux no casaban con los rangos del NVD**
  (`F-14`, corregido en el servidor). El sufijo de empaquetado de Debian hacía
  que `2.39-0ubuntu8.3` ordenase por debajo de `2.39`, así que un rango
  "vulnerable desde 2.39" no casaba y la vulnerabilidad no se reportaba. El
  adaptador a Lybra usa ahora la versión de origen; el inventario guardado
  conserva la versión exacta del paquete.
- **El inventario de Windows no veía el software instalado por el usuario**
  (`A-12`). Se leía `HKEY_CURRENT_USER`, que para un servicio corriendo como
  `LocalSystem` es la rama de `LocalSystem` —cuya clave `Uninstall` ni
  siquiera existe—, no la del usuario del escritorio. Ahora se enumera
  `HKEY_USERS`. En la máquina de prueba aparecen 13 programas que antes se
  perdían, entre ellos Discord, Spotify, GitHub Desktop y DBeaver. La
  arquitectura, además, ya no se reporta siempre como `x64`.

- **El heartbeat se descartaba entero cuando un proceso usaba más de un núcleo**
  (`A-01`). El porcentaje de CPU por proceso no se acotaba, y el backend rechaza
  con `422` cualquier porcentaje fuera de `[0,100]`. El activo se quedaba mudo
  justo cuando tenía carga alta. `cpuPct` pasa a ser porcentaje de la capacidad
  total del equipo, y todos los porcentajes del payload se acotan en un único
  punto.
- **El buffer en disco era decorativo ante caídas de más de cinco minutos**
  (`A-02`). El agente retenía cuatro horas de heartbeats y el backend rechazaba
  todo lo de más de 300 s. La ventana de reloj del backend pasa a ser
  asimétrica: corta hacia el futuro, 24 h hacia el pasado.
- **Drenar el buffer bloqueaba el bucle principal** (`A-03`). El `429` por
  cadencia se trataba como un fallo de red y disparaba el backoff exponencial:
  drenar 3 payloads costaba 15,03 s y ahora cuesta 0,02 s. El drenado está
  acotado por número y por tiempo.
- **Un inventario de más de 2000 aplicaciones tiraba el heartbeat entero, cada
  seis horas, para siempre** (`A-04`). Se acota en el agente, y el inventario ya
  no se pierde si el payload que lo llevaba acaba descartado.
- **El fichero de log crecía sin límite** (`A-09`), unos 350 MB al año por
  equipo. Ahora rota a 5 MB y se registran los cambios de estado en vez de un
  heartbeat correcto cada quince segundos.
- **La CI instalaba Go 1.25.0 exacto** (`A-10`), de agosto de 2025, porque
  `setup-go` trata la directiva `go` del `go.mod` como una versión exacta. Se
  perdían doce parches, varios de seguridad. Ahora se fija `1.25.x`.

- **El build de macOS llevaba semanas roto por un fichero de recursos de
  Windows.** `rsrc.syso` (el icono y los metadatos del `.exe`) no llevaba
  sufijo de plataforma en el nombre, así que Go lo enlazaba en **todos** los
  sistemas. En macOS eso rompía el enlazado con un error que ni siquiera
  nombraba el fichero (`ld: unknown file type in '.../000000.o'`). Renombrado a
  `rsrc_windows.syso`, que es el nombre que Go usa para restringirlo a Windows.
  La CI comprueba ahora que ningún `.syso` se quede sin sufijo.
- **Los tests del canal de control fallaban en macOS** por una ruta de socket
  demasiado larga. La dirección de un socket Unix viaja en un campo de tamaño
  fijo (104 bytes en macOS, 108 en Linux) y los tests la construían bajo
  `t.TempDir()`, que en macOS cuelga de `/var/folders/…` y suma 45 caracteres
  más que en Linux. Afectaba solo a los tests: en producción el socket es
  `/run/hygeia-agent.sock`, 22 bytes. `Listen()` explica ahora el problema en
  vez de propagar el `invalid argument` del sistema, y hay una prueba que
  comprueba el límite en todas las plataformas tipo Unix, no solo donde
  aprieta.

### Rendimiento

- **Un ciclo de recolección pasa de 1001 ms a 27,5 ms** (`A-06`). El recolector
  de CPU dormía un segundo en cada ciclo para muestrear; ahora calcula por
  diferencia entre ciclos, como ya hacían los de red y procesos.
- **`buffer.Len()` pasa de 1,51 ms a 45 ns** y drenar mil payloads, de 7,47 s a
  1,58 s (`A-07`). El buffer leía y reescribía el fichero entero en cada
  operación, incluido el recuento que el icono de bandeja pide cada cinco
  segundos.
- **El recolector de procesos deja de consultar la memoria total del sistema una
  vez por proceso** (`A-05`) y de ordenar el vector completo dos veces para
  elegir diez elementos (`A-08`). En Linux, un 18 % más rápido; en Windows, sin
  cambio medible.

### Añadido

- `bufferMaxItems`, `inventoryMaxItems` y `logLevel` en la configuración.
- Comprobación de formato, `go mod tidy`, `golangci-lint` y `govulncheck` en la
  integración continua (`O-03`).
- `docs/CONTRATO-INGESTA.md`: fuente única del contrato con el backend, hasta
  ahora duplicado y divergente (`A-13`).

### Cambiado

- Los paquetes del núcleo se mueven a `internal/` (`O-01`).
- El README se parte en README, `docs/DISENO.md` y `docs/CONTRATO-INGESTA.md`
  (`O-02`).

### Cambios que rompen compatibilidad

- **`metrics.processes.topCpu[].cpuPct` cambia de escala.** Antes era porcentaje
  de un núcleo (como `top`: un proceso con dos núcleos saturados daba `200`);
  ahora es porcentaje de la capacidad total del equipo, con `100` como máximo.
  Los datos históricos anteriores a este cambio están en la escala vieja.
