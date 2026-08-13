# Registro de cambios

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/).
Las versiones publicadas se anotan en [`VERSION.txt`](VERSION.txt).

Los identificadores `A-nn`, `O-nn` y `F-nn` remiten a
[`docs/ANALISIS-INGENIERIA.md`](docs/ANALISIS-INGENIERIA.md).

## [Sin publicar]

### Corregido

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

### Conocido, sin resolver

- **El build de macOS falla en la integración continua sobre la imagen
  `macos-26-arm64`**, con `ld: unknown file type`. No es una regresión: ese job
  no ha pasado nunca, y el fallo se reproduce igual con Go 1.24.0, 1.25.0 y
  1.25.12. Mientras se investiga, la matriz incluye también `macos-15`, que sí
  da cobertura real, y `macos-latest` se ejecuta tolerando el fallo.

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
