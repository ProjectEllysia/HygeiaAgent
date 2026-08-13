# Hygeia — Análisis de ingeniería del agente

> Documento de análisis técnico del repositorio `HygeiaAgent`, escrito el 12 de agosto de 2026
> contra el commit `65b9206` (rama `develope`).
>
> Cubre tres cosas: (1) los algoritmos que se pueden mejorar por lentitud o por deuda técnica,
> (2) la organización de ficheros del repositorio, y (3) las funcionalidades nuevas que el
> agente podría llegar a tener, teniendo en cuenta lo que el módulo `hygeia` del servidor
> (`EllysiaServer/API/src/modules/features/hygeia`) ya sabe recibir y hacer.
>
> Todo lo que aparece aquí está verificado leyendo el código de ambos repositorios y, cuando
> hacía falta, el código de las librerías de terceros. Cada punto indica el fichero y la línea
> concretos para que se pueda comprobar.

---

## Índice

1. [Cómo leer este documento](#1-cómo-leer-este-documento)
2. [Resumen ejecutivo](#2-resumen-ejecutivo)
3. [Parte 1 — Algoritmos, rendimiento y deuda técnica](#3-parte-1--algoritmos-rendimiento-y-deuda-técnica)
4. [Parte 2 — Organización de ficheros](#4-parte-2--organización-de-ficheros)
5. [Parte 3 — Funcionalidades nuevas](#5-parte-3--funcionalidades-nuevas)
6. [Parte 4 — Catálogo completo ordenado](#6-parte-4--catálogo-completo-ordenado)
7. [Parte 5 — Agrupación en fases](#7-parte-5--agrupación-en-fases)
8. [Anexo A — Cómo comprobar cada hallazgo](#8-anexo-a--cómo-comprobar-cada-hallazgo)

---

## 1. Cómo leer este documento

Cada oportunidad detectada lleva un identificador estable, para poder referirse a ella en un
issue o en una conversación sin tener que repetir la descripción entera:

| Prefijo | Significado |
|---|---|
| `A-nn` | Algoritmo, rendimiento o corrección de comportamiento |
| `O-nn` | Organización del repositorio, ficheros y proceso de desarrollo |
| `F-nn` | Funcionalidad nueva |

Cada oportunidad se cataloga con dos escalas independientes.

**Impacto en el usuario.** Se refiere al usuario final de Ellysia (la persona que mira el
panel de activos), no al desarrollador:

| Nivel | Qué significa |
|---|---|
| **Crítico** | Hoy se están perdiendo datos, o el usuario ve información falsa, o una funcionalidad anunciada no funciona en absoluto. |
| **Alto** | El usuario nota una degradación clara: el equipo va más lento, faltan datos, o necesita intervención manual para algo que debería ser automático. |
| **Medio** | El usuario se beneficia, pero puede vivir sin ello. Típicamente más información o más comodidad. |
| **Bajo** | El usuario no lo percibe directamente. Beneficia sobre todo al equipo de desarrollo o al mantenimiento futuro. |

**Facilidad de implementación.** Estimación de esfuerzo para una persona que ya conoce el
repositorio:

| Nivel | Qué significa |
|---|---|
| **Muy fácil** | Menos de medio día. Cambio localizado en uno o dos ficheros, sin decisiones de diseño pendientes. |
| **Fácil** | Uno o dos días. Cambio localizado pero con pruebas nuevas que escribir. |
| **Media** | Entre tres y cinco días. Toca varios paquetes, o requiere decidir algo de diseño antes de escribir código. |
| **Difícil** | Más de una semana, o requiere cambios coordinados en el repositorio del servidor además del agente. |

---

## 2. Resumen ejecutivo

El agente está, en general, bien construido: la separación en paquetes es limpia, hay pruebas
en casi todos ellos, la integración continua compila y prueba en los tres sistemas operativos,
y las decisiones de diseño están documentadas en comentarios de calidad poco habitual. El
código no es el problema.

El problema está en la **costura con el servidor**. El agente y el módulo `hygeia` del backend
se diseñaron con documentos separados que describen el mismo contrato de ingesta, y ese
contrato ha ido divergiendo. De esa divergencia salen los tres hallazgos más graves de todo
este análisis, y los tres provocan **pérdida silenciosa de datos**: el agente cree que ha
enviado, el servidor rechaza, y nadie se entera.

Los tres, en orden de gravedad:

1. **`A-01`** — El agente calcula el porcentaje de CPU de un proceso como porcentaje del tiempo
   total transcurrido, sin acotarlo. En una máquina de varios núcleos, un proceso que use dos
   núcleos al máximo produce el valor `200.0`. El esquema del servidor valida ese campo con
   `Range(min=0, max=100)`, así que rechaza **el heartbeat entero** con un error de validación.
   El agente clasifica ese error como permanente y **descarta el envío**. Es decir: en el
   momento exacto en que un equipo tiene un problema de CPU —justo cuando la monitorización
   importa— el agente deja de reportar. Y el ejemplo que el propio README pone para justificar
   la lista de procesos destacados es un minero de criptomonedas, que es precisamente el tipo
   de proceso que satura varios núcleos.

2. **`A-02`** — El buffer en disco guarda hasta mil heartbeats (unas cuatro horas de histórico
   con el intervalo por defecto de quince segundos), pero el servidor rechaza cualquier
   heartbeat cuyo `collectedAt` se aleje más de trescientos segundos de su propio reloj. De los
   mil que caben, como mucho los veinte últimos podrán entregarse; el resto se descartan uno a
   uno al drenar. El mecanismo de resiliencia que el README describe como innegociable está,
   en la práctica, limitado a cortes de menos de cinco minutos.

3. **`A-03`** — Al recuperar la conexión, el agente drena el buffer enviando los payloads uno
   detrás de otro en un bucle cerrado. El servidor impone un intervalo mínimo de cinco segundos
   entre heartbeats de la misma clave y responde `429` si se incumple. El agente interpreta ese
   `429` como un fallo transitorio y reintenta con espera exponencial, de modo que cada payload
   del buffer cuesta unos siete segundos de bloqueo **dentro del bucle principal**, durante los
   cuales el agente no recolecta ni envía nada nuevo.

Después de eso, el segundo bloque de mejoras es el **coste del agente sobre el equipo
monitorizado**: el recolector de procesos hace tres o cuatro llamadas al sistema por proceso y,
entre ellas, lee la memoria total del sistema una vez por cada proceso; el recolector de CPU
bloquea un segundo entero en cada ciclo; y el buffer en disco reescribe el fichero completo en
cada operación, incluso al contar cuántos elementos tiene, cosa que el icono de bandeja pide
cada cinco segundos.

En organización de ficheros, la estructura actual es **correcta y perfectamente idiomática en
Go**; no hay nada roto. Lo que hay son tres mejoras que ganan claridad conforme el proyecto
crezca: mover los paquetes internos bajo `internal/`, romper el README en tres documentos
(porque hoy hace de manual de uso, documento de diseño y copia del contrato de ingesta a la
vez, y esa copia ya está desactualizada), y arreglar un `go.mod` que tiene dos dependencias
declaradas con una versión inválida que impide ejecutar `go mod tidy`.

En funcionalidades, hay una lista larga, pero la que más valor desbloquea por unidad de
esfuerzo es el **inventario de software en Linux y macOS**: hoy son funciones vacías que
devuelven una lista vacía, mientras que toda la tubería del servidor —persistencia, informe en
PDF, y el análisis de vulnerabilidades con el motor Lybra— ya está construida y funcionando
para Windows.

---

## 3. Parte 1 — Algoritmos, rendimiento y deuda técnica

### 3.1. Corrección del contrato de ingesta (pérdida de datos)

---

#### `A-01` — El porcentaje de CPU por proceso puede superar 100 y hace que el servidor rechace el heartbeat completo

**Impacto en el usuario: Crítico · Facilidad: Muy fácil**

**Qué ocurre hoy.** En [`collector/processes.go:91`](../collector/processes.go) el porcentaje de
CPU de cada proceso se calcula así:

```go
cpuPct = 100 * (total - prev) / elapsed
```

donde `total` y `prev` son segundos de CPU consumidos, sumando todos los núcleos, y `elapsed`
son los segundos de reloj transcurridos desde la medición anterior. Es la fórmula estándar de
la industria (es lo mismo que muestra `top` en Linux) y **es correcta**: en una máquina de
ocho núcleos, un proceso puede legítimamente consumir hasta 800 por ciento.

El problema está en el otro extremo. El esquema de validación del servidor, en
`EllysiaServer/API/src/modules/features/hygeia/schemas.py:180`, declara:

```python
cpuPct = fields.Float(load_default=None, validate=validate.Range(min=0, max=100))
```

**Por qué es un problema.** La cadena de consecuencias es la siguiente:

1. Un proceso consume dos núcleos al máximo durante un intervalo de quince segundos.
2. El agente calcula `cpuPct = 200.0`.
3. Como la lista `topCpu` está ordenada de mayor a menor, ese proceso entra seguro en el top 5.
4. El servidor devuelve un error de validación (`422`, o `400` según la ruta exacta de
   Marshmallow).
5. En [`shipper/shipper.go:81`](../shipper/shipper.go), `isPermanentStatus` clasifica ambos
   códigos como permanentes.
6. En [`agent/agent.go:316`](../agent/agent.go), un error permanente hace que el payload se
   **descarte sin guardarlo en el buffer**.

El resultado es que el activo se queda mudo exactamente durante los periodos de carga alta.
Peor todavía: como el heartbeat nunca llega, el detector de presencia del servidor acabará
marcando el activo como `offline` y abriendo una anomalía `host_down`, que es un diagnóstico
completamente equivocado — el equipo está encendido y saturado, no caído.

**Qué hacer.** Hay que decidir cuál de los dos lados tiene razón, y hay un argumento claro para
cada opción:

- **Opción recomendada (agente):** acotar el valor en el agente al normalizar por número de
  núcleos, es decir, dividir entre `runtime.NumCPU()`. Así `cpuPct` pasa a significar
  "porcentaje de la capacidad total de CPU de la máquina", que es coherente con
  `metrics.cpu.usagePct` (que ya es la media de todos los núcleos) y permite que las dos cifras
  se comparen entre sí en el panel. Un proceso que satura la máquina entera da 100, no 800.
- **Opción alternativa (servidor):** subir el límite del esquema a, por ejemplo, `6400` (cien
  por núcleo hasta sesenta y cuatro núcleos) y documentar que `cpuPct` es "porcentaje de un
  núcleo", como `top`.

Sea cual sea la decisión, el agente debe **acotar el valor antes de enviarlo** en cualquier
caso, porque un fallo de reloj (un salto hacia atrás, una hibernación) puede producir valores
absurdos que ninguna elección de esquema cubriría.

El cambio en el agente es de tres líneas en `toProcessInfo`. La prueba que lo acompaña es
igual de corta: simular un delta de CPU mayor que el tiempo transcurrido y comprobar que el
valor emitido no supera el límite.

---

#### `A-02` — El buffer en disco guarda mil heartbeats, pero el servidor solo acepta los de los últimos cinco minutos

**Impacto en el usuario: Crítico · Facilidad: Fácil**

**Qué ocurre hoy.** El agente crea el buffer con capacidad para mil elementos, valor fijado en
el código y no configurable, en [`agent/agent.go:55`](../agent/agent.go):

```go
buf: buffer.NewRingBuffer(cfg.BufferPath, 1000),
```

Con el intervalo por defecto de quince segundos, mil elementos equivalen a **cuatro horas y
diez minutos** de histórico retenido durante una caída del backend.

Por el otro lado, el servidor aplica una ventana de cordura de reloj en
`services/ingest_guard.py:97` (`check_clock_skew`), con el valor configurado en
`API/SecOpsConfig.json`:

```json
"clockSkewSec": 300
```

Cualquier heartbeat cuyo `collectedAt` se desvíe más de **trescientos segundos** del reloj del
servidor se rechaza con `IngestClockSkewError`, que es un `400`.

**Por qué es un problema.** `400` está en la lista de estados permanentes del agente, así que
al drenar el buffer tras una caída de más de cinco minutos, los payloads antiguos se descartan
uno a uno con un aviso en el log. De los mil huecos del buffer, solo los veinte últimos
contienen datos que el servidor vaya a aceptar. Los otros novecientos ochenta ocupan disco,
cuestan entrada y salida en cada operación (véase `A-07`) y están garantizados como basura.

Esto no es un fallo de implementación de ninguno de los dos lados: cada uno hace lo que su
propio documento de diseño dice. Es una **contradicción entre los dos documentos** que nadie ha
resuelto porque viven en repositorios distintos.

**Qué hacer.** De nuevo, hay que elegir qué significa el buffer, y las dos opciones son
legítimas:

- **Si el buffer sirve para no perder ni un dato durante una caída larga**, entonces el
  servidor tiene que aceptar payloads antiguos. La forma limpia de hacerlo es reconocer que el
  servidor **ya guarda `received_at` aparte de `collected_at`** y ya ordena la serie temporal
  por `received_at` (está documentado explícitamente en `schemas.py:343` y en el §16.3 del plan
  del backend). Es decir: el motivo original de la ventana de reloj —evitar que una clave
  robada envenene el orden de la serie— **ya está resuelto por otra vía**. La ventana podría
  ampliarse mucho (por ejemplo a veinticuatro horas) sin perder ninguna garantía real. Ese es
  el camino recomendado.
- **Si el buffer solo sirve para absorber microcortes**, entonces la capacidad de mil está mal
  y debería ser del orden de treinta elementos, y el README debería dejar de prometer
  resiliencia ante caídas largas.

Además, y con independencia de lo anterior: **el tamaño del buffer debería salir de la
configuración**, no estar escrito a fuego en `agent.New`. Un servidor de producción y un
portátil no tienen por qué guardar lo mismo.

---

#### `A-03` — El drenado del buffer choca con el intervalo mínimo del servidor y bloquea el bucle principal

**Impacto en el usuario: Alto · Facilidad: Media**

**Qué ocurre hoy.** [`agent/agent.go:389`](../agent/agent.go), función `drainBuffer`, envía los
payloads pendientes en un bucle cerrado, uno detrás de otro, sin ninguna pausa entre ellos.

El servidor, en `managers.py:836` (`_enforce_min_interval`), rechaza con `429` cualquier
heartbeat que llegue antes de `minIntervalSec` segundos desde el `last_seen_at` del activo. El
valor configurado es cinco segundos.

**Por qué es un problema.** El `429` **no** está en la lista de estados permanentes, así que el
agente lo trata como un fallo transitorio y aplica su espera exponencial: intenta en el
segundo cero, espera uno, intenta, espera dos, intenta, espera cuatro, intenta. En ese cuarto
intento ya han pasado siete segundos desde el último heartbeat aceptado, así que el envío
funciona. El resultado neto es que **cada payload del buffer cuesta unos siete segundos**, y
tres cuartas partes de los intentos son peticiones rechazadas que el agente hace igualmente.

Y el problema mayor: `drainBuffer` se llama desde `runOnce`, que se llama desde `tick`, que se
llama **de forma síncrona** desde el bucle del ticker en `Run`. Mientras el agente drena, no
recolecta ni envía métricas nuevas. Drenar cien payloads deja al agente sin reportar durante
casi doce minutos. Drenar los mil que caben lo dejaría fuera casi dos horas.

**Qué hacer.** Tres cambios, de menor a mayor ambición:

1. **Limitar cuántos payloads se drenan por ciclo.** Es el arreglo mínimo y evita el bloqueo
   largo: drenar como mucho tres o cuatro por tick, y dejar el resto para el siguiente. Una
   línea de código.
2. **Tratar el `429` como lo que es.** No es un fallo de red, es el servidor diciendo "espera".
   Lo correcto es no reintentar dentro del mismo `Send`, sino abortar el drenado de este ciclo
   y esperar al siguiente. Si además el servidor devolviera la cabecera `Retry-After`, el
   agente podría respetarla exactamente. Requiere distinguir un tercer tipo de error en el
   `shipper`, junto a los transitorios y los permanentes.
3. **Endpoint de ingesta por lotes.** La solución de fondo: un `POST /hygeia/ingest/batch` que
   acepte un array de heartbeats en una sola petición. Elimina de raíz el conflicto con el
   intervalo mínimo, reduce el número de peticiones y de negociaciones TLS, y hace que drenar
   una caída larga sea una sola operación en lugar de mil. Requiere trabajo en ambos
   repositorios, y por eso es la opción de una fase posterior.

---

#### `A-04` — El inventario de software no tiene tope local y el servidor rechaza los que superan dos mil elementos

**Impacto en el usuario: Alto · Facilidad: Muy fácil**

**Qué ocurre hoy.** [`collector/inventory_windows.go:47`](../collector/inventory_windows.go)
recorre las tres ubicaciones del registro de Windows donde se anota el software instalado y
devuelve todo lo que encuentre con un `DisplayName` no vacío, sin límite.

El servidor, en `schemas.py:260` (`InventorySchema.validate_max_items`), rechaza cualquier
inventario con más de `maxInventoryItems` elementos. El valor configurado es **dos mil**.

**Por qué es un problema.** En una estación de trabajo de desarrollo con muchos paquetes
redistribuibles de Visual C++, muchos parches y muchas extensiones, superar dos mil entradas es
perfectamente posible. Cuando ocurre, el servidor responde con un error de validación, que es
permanente, así que **el heartbeat entero se descarta** — no solo el inventario. Y como el
inventario se vuelve a escanear cada seis horas y sigue teniendo el mismo tamaño, el fallo se
repite indefinidamente: ese activo pierde un heartbeat cada seis horas para siempre, y nunca
llega a tener inventario.

Un detalle agravante: el inventario se adjunta al primer heartbeat después de cada escaneo
([`agent/agent.go:366`](../agent/agent.go)) y se limpia acto seguido. Si ese heartbeat concreto
falla, el inventario **se pierde por completo** hasta el escaneo siguiente, aunque el fallo no
tuviera nada que ver con el inventario.

**Qué hacer.** Dos cosas, ambas cortas:

- Acotar la lista en el agente antes de enviarla, con el límite en la configuración y un valor
  por defecto por debajo del del servidor (por ejemplo mil quinientos). Si se recorta, registrar
  en el log cuántas entradas se dejaron fuera, para que el hecho no sea invisible.
- Cambiar el consumo del inventario para que sea **destructivo solo tras un envío
  confirmado**. Hoy `collectPayload` limpia `lastInventory` en el momento de montar el payload,
  antes de saber si el envío va a funcionar. Debería limpiarse en `runOnce`, después de que
  `Send` haya devuelto correctamente.

---

### 3.2. Coste del agente sobre el equipo monitorizado

---

#### `A-05` — El recolector de procesos lee la memoria total del sistema una vez por cada proceso

**Impacto en el usuario: Alto · Facilidad: Fácil**

**Qué ocurre hoy.** En [`collector/processes.go:96`](../collector/processes.go), dentro del
bucle que recorre todos los procesos:

```go
mem, _ := p.MemoryPercentWithContext(ctx)
```

La implementación de `gopsutil` (verificada en
`gopsutil/v4@v4.26.6/process/process.go:342`) es:

```go
func (p *Process) MemoryPercentWithContext(ctx context.Context) (float32, error) {
	machineMemory, err := mem.VirtualMemoryWithContext(ctx)   // <-- memoria TOTAL del sistema
	...
	processMemory, err := p.MemoryInfoWithContext(ctx)        // <-- memoria de ESTE proceso
	...
	return (100 * float32(used) / float32(total)), nil
}
```

Es decir, **cada llamada consulta la memoria total de la máquina**, un dato que no cambia
durante la ejecución.

**Por qué es un problema.** En una máquina con trescientos procesos, cada ciclo de recolección
hace trescientas consultas de memoria total del sistema en lugar de una. En Windows son
trescientas llamadas a `GlobalMemoryStatusEx`; en Linux son **trescientas lecturas del fichero
`/proc/meminfo`**, que además implica que el núcleo formatee ese fichero trescientas veces.

Y no es lo único que se repite por proceso. El bucle hace además, para **cada** proceso:

- `StatusWithContext` (línea 77), para contar zombis. En Linux esto lee `/proc/<pid>/status`.
  En Windows, `gopsutil` devuelve siempre un error de "no implementado"
  (`process_windows.go:464`), así que en Windows el contador de zombis es **siempre cero** y la
  llamada no aporta nada.
- `TimesWithContext` (línea 86), que en Linux lee `/proc/<pid>/stat`.
- `MemoryPercentWithContext` (línea 96), que como acabamos de ver son dos lecturas más.

En total, unas cuatro o cinco operaciones de lectura por proceso, cuando bastan dos.

**Qué hacer.**

1. **Sacar la memoria total del bucle.** Llamar a `mem.VirtualMemoryWithContext` una sola vez
   antes de recorrer los procesos y usar `p.MemoryInfoWithContext(ctx).RSS / total` dentro. Es
   el mismo cálculo, con una consulta en lugar de N.

> **Medición posterior (implementado el 13 de agosto de 2026).** La estimación original de
> este apartado —"en torno al cuarenta por ciento"— era demasiado optimista y solo vale para
> Linux. Con el benchmark `BenchmarkProcessCollectorCollect`, medido antes y después:
>
> | Sistema | Antes (mediana) | Después (mediana) | Cambio |
> |---|---|---|---|
> | Linux, 27 procesos | 2,64 ms | 2,17 ms | **−18 %** |
> | Windows, ~250 procesos | 25,4 ms | 26,3 ms | sin cambio (dentro del ruido) |
>
> El motivo de la diferencia es que el coste que se elimina no es el mismo en los dos
> sistemas. En Linux, `MemoryPercent` provoca una lectura de `/proc/meminfo` por proceso, y
> eso sí es caro. En Windows, la llamada equivalente es `GlobalMemoryStatusEx`, que es
> barata, y `StatusWithContext` ni siquiera llega a hacer una llamada al sistema porque
> `gopsutil` devuelve "no implementado" de inmediato. En Windows, el coste dominante está en
> `ProcessesWithContext` y en abrir un manejador por proceso, que este cambio no toca.
>
> El cambio se conserva porque es una mejora clara donde importa (servidores Linux, donde
> además el ahorro crece con el número de procesos) y no empeora nada en Windows. Pero la
> cifra del cuarenta por ciento era una estimación de despacho, no una medida.
2. **No pedir el estado del proceso en Windows.** El contador de zombis solo tiene sentido en
   sistemas de tipo Unix. Se puede resolver con una variable a nivel de paquete definida por
   sistema operativo (el repositorio ya usa ese patrón para `inventory_*.go`), de modo que en
   Windows el bucle ni siquiera intente la llamada. Elimina otra lectura por proceso en Linux
   sin perder nada, y en Windows quita una llamada que siempre falla.

---

#### `A-06` — El recolector de CPU bloquea un segundo entero en cada ciclo

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Qué ocurre hoy.** [`collector/cpu.go:27`](../collector/cpu.go):

```go
perCore, err := gpscpu.PercentWithContext(ctx, sampleInterval(), true)   // sampleInterval() == 1s
```

`gopsutil` implementa esa llamada tomando una muestra de los contadores del sistema,
**durmiendo el intervalo indicado**, tomando otra muestra y calculando la diferencia. Un
segundo de bloqueo real por cada ciclo.

**Por qué es un problema.** Con el intervalo por defecto de quince segundos, el agente pasa
**una de cada quince unidades de tiempo bloqueado** en esa llamada. Aunque el recolector corre
en su propia goroutine y no bloquea a los demás, sí determina la duración mínima de un ciclo
completo y consume la cuota del `context.WithTimeout` de cinco segundos que
[`agent/agent.go:377`](../agent/agent.go) le concede.

Hay además una inconsistencia de diseño: los recolectores de red y de procesos **ya calculan
tasas guardando la muestra anterior entre ciclos**, exactamente para no tener que bloquear. El
de CPU es el único que no lo hace.

**Qué hacer.** Aplicarle al recolector de CPU el mismo patrón que ya usan `network.go` y
`processes.go`: guardar el resultado de `cpu.TimesWithContext` del ciclo anterior en la
estructura del recolector y calcular el porcentaje como diferencia entre ciclos. Ventajas:

- Desaparece el bloqueo de un segundo.
- El porcentaje pasa a ser la media sobre el intervalo completo en lugar de una foto de un
  segundo. Para la detección de anomalías del servidor esto es **mejor**, no peor: los umbrales
  usan `sustainedHeartbeats: 3`, es decir, buscan carga sostenida, no picos instantáneos.
- El primer ciclo no tendrá dato de CPU, igual que hoy ya ocurre con la red y con el top de
  procesos. Es un comportamiento ya establecido en el repositorio y el servidor ya lo tolera
  (todos los campos de la serie temporal son opcionales).

Ojo: `metrics.cpu.usagePct` es obligatorio en el esquema del servidor
(`schemas.py:194`, `cpu = fields.Nested(CpuMetricsSchema, required=True)`), así que el primer
heartbeat sin datos de CPU sería rechazado. La forma limpia de resolverlo es que el primer
ciclo siga usando el muestreo bloqueante y los siguientes usen diferencias — o, más simple,
que el agente tome la muestra base durante el jitter de arranque, que ya existe y ya espera.

---

#### `A-07` — El buffer en disco reescribe el fichero completo en cada operación, incluso al contarlo

**Impacto en el usuario: Alto · Facilidad: Media**

**Qué ocurre hoy.** [`buffer/buffer.go`](../buffer/buffer.go) implementa el ring buffer sobre un
fichero JSONL, y las tres operaciones públicas hacen lo mismo:

| Operación | Qué hace realmente | Coste |
|---|---|---|
| `Push` (línea 94) | Lee **todo** el fichero a memoria, añade una línea, escribe **todo** el fichero a un temporal y lo renombra | Lectura de N + escritura de N |
| `Pop` (línea 115) | Lee **todo** el fichero, decodifica la primera línea, escribe **todo** el fichero menos esa línea | Lectura de N + escritura de N |
| `Len` (línea 139) | Lee **todo** el fichero solo para contar las líneas | Lectura de N |

**Por qué es un problema.** Tres escenarios concretos, con el buffer lleno (mil payloads de unos
tres kilobytes cada uno, es decir, un fichero de unos tres megabytes):

- **El icono de bandeja.** `hygeia-tray` consulta el estado cada cinco segundos
  ([`cmd/hygeia-tray/main.go:33`](../cmd/hygeia-tray/main.go)), y `Agent.Status()` llama a
  `buf.Len()` ([`agent/agent.go:76`](../agent/agent.go)). Resultado: **el servicio lee tres
  megabytes de disco cada cinco segundos** para mostrar un número en un menú. Son unos
  cincuenta megabytes por minuto de lectura, dos gigabytes por hora, indefinidamente. En un
  portátil con disco cifrado esto es perfectamente perceptible.
- **El drenado.** Vaciar el buffer entero son mil `Pop`, cada uno con su lectura y su escritura
  completas. El total es del orden de **seis gigabytes de entrada y salida** para mover tres
  megabytes de datos. Es un comportamiento cuadrático: el coste crece con el cuadrado del
  número de elementos.
- **La acumulación durante una caída.** Cada `Push` cuesta más que el anterior, porque el
  fichero es más largo.

**Qué hacer.** No hace falta reescribir el buffer con un formato binario ni con un índice en
disco. Tres cambios sencillos, en orden de importancia:

1. **Mantener el recuento en memoria.** El buffer es el único que escribe en ese fichero; puede
   llevar un contador propio, cargado la primera vez y actualizado en cada `Push` y `Pop`.
   `Len()` pasa de leer tres megabytes a devolver un entero. Es el arreglo de mayor beneficio y
   menor riesgo de todos los de este documento.
2. **Añadir un `PopBatch(n)`.** Una sola lectura, se extraen `n` elementos, una sola escritura.
   Encaja de forma natural con el límite de drenado por ciclo que propone `A-03`.
3. **Hacer que `Push` sea una operación de añadir al final.** Abrir el fichero en modo
   `O_APPEND` y escribir una línea es una operación de coste constante. Solo hace falta leer y
   reescribir el fichero cuando toca rotar (cuando se supera el máximo), y esa rotación se puede
   hacer por lotes: en lugar de recortar un elemento cada vez, recortar el diez por ciento
   cuando se llegue al tope. Así el coste amortizado por `Push` es constante.

Con esos tres cambios, el buffer pasa de cuadrático a lineal sin cambiar su formato en disco ni
sus garantías, y las pruebas existentes en `buffer/buffer_test.go` siguen siendo válidas.

---

#### `A-08` — El top-5 de procesos se obtiene ordenando dos veces la lista completa

**Impacto en el usuario: Bajo · Facilidad: Muy fácil**

**Qué ocurre hoy.** [`collector/processes.go:107` y `:111`](../collector/processes.go) ordenan
el vector entero de procesos, primero por CPU y después por memoria, para quedarse con los
cinco primeros de cada uno.

**Por qué es un problema.** Ordenar trescientos elementos dos veces son unas cinco mil
comparaciones para elegir diez valores. No es un problema de rendimiento serio —comparado con
las mil llamadas al sistema del punto `A-05` es ruido— pero es una ineficiencia gratuita.

**Qué hacer.** Un recorrido único manteniendo los cinco mayores de cada criterio. Con `topN = 5`
basta una inserción lineal en un vector de cinco posiciones; no hace falta un montículo. La
biblioteca estándar también ofrece `slices.SortFunc`, que es más rápido que `sort.Slice` porque
evita la reflexión, si se prefiere el cambio mínimo.

Se menciona aquí por completitud y porque es el tipo de cambio que se hace de paso al tocar
`A-05`, no porque merezca una tarea propia.

---

#### `A-09` — El fichero de log crece sin límite

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Qué ocurre hoy.** [`cmd/hygeia-agent/main.go:149`](../cmd/hygeia-agent/main.go) abre el
fichero de log en modo añadir y no lo rota nunca:

```go
if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
```

Y [`agent/agent.go:326`](../agent/agent.go) escribe una línea de nivel `Info` **en cada
heartbeat correcto**:

```go
a.log.Info("heartbeat enviado", "nextIntervalSec", resp.NextIntervalSec)
```

**Por qué es un problema.** A quince segundos por heartbeat son cinco mil setecientas sesenta
líneas al día, en torno a un megabyte diario, unos trescientos cincuenta megabytes al año, por
cada equipo de la flota. En un servidor con partición `/` pequeña esto llega a llenar el disco,
lo que resulta especialmente irónico porque el propio agente está ahí para avisar de discos
llenos. Además contradice de forma directa el principio que el README §5 declara innegociable
("nunca RAM ni disco ilimitados"), que se respetó escrupulosamente para el buffer y para el
`logring`, pero no para el fichero de log.

**Qué hacer.** Dos medidas complementarias:

1. **Bajar el ruido.** El heartbeat correcto es el caso normal; no necesita una línea de nivel
   `Info` cada quince segundos. Basta con registrar los **cambios de estado** (pasar a
   conectado, pasar a error, cambiar de intervalo) y dejar el heartbeat individual en nivel
   `Debug`. Esto por sí solo reduce el volumen en más del noventa y nueve por ciento y es un
   cambio de una línea.
2. **Acotar el fichero.** Un envoltorio de `io.Writer` de unas treinta líneas que compruebe el
   tamaño y, al superar un umbral (por ejemplo cinco megabytes), renombre el fichero a
   `hygeia-agent.log.1` y empiece uno nuevo. No hace falta añadir una dependencia como
   `lumberjack` para conservar un único fichero anterior.

---

### 3.3. Deuda técnica y datos incompletos

---

#### `A-10` — El fichero `go.mod` declara dos dependencias con una versión inválida

**Impacto en el usuario: Bajo · Facilidad: Muy fácil**

**Qué ocurre hoy.** [`go.mod:14-15`](../go.mod):

```
    github.com/antandros/go-dpkg v0.0.0-latest
    github.com/knqyf263/go-rpmdb v0.0.0-latest
```

`v0.0.0-latest` no es una versión válida en el sistema de módulos de Go. Además, ninguno de los
dos módulos está importado por ningún paquete del repositorio: se añadieron anticipando el
inventario de Linux (`F-01`) y se quedaron ahí.

**Por qué es un problema.** `go build ./...` funciona, porque Go solo resuelve los módulos que
alguien importa de verdad. Pero cualquier comando que cargue el grafo completo de módulos
falla:

```
$ go mod verify
go: github.com/antandros/go-dpkg@v0.0.0-latest: reading go.mod at revision v0.0.0-latest: unknown revision

$ go list -m all
go: github.com/antandros/go-dpkg@v0.0.0-latest: invalid version: unknown revision v0.0.0-latest
```

Esto significa que **`go mod tidy` no se puede ejecutar**, que las herramientas de análisis de
dependencias y de vulnerabilidades (`govulncheck`, Dependabot) no pueden funcionar, y que el
primer día que alguien vaya a implementar el inventario de Linux se encontrará con un error
que no tiene nada que ver con lo que iba a hacer.

La integración continua no lo detecta porque solo ejecuta `go vet ./...` y `go build ./...`
([`.github/workflows/ci.yml:29,38`](../.github/workflows/ci.yml)), y ninguno de los dos carga el
grafo completo.

**Qué hacer.** Quitar las dos líneas ahora (se vuelven a añadir con `go get` el día que se
implemente `F-01`) y añadir a la integración continua un paso que lo habría cazado:

```yaml
- name: go.mod está al día
  run: go mod tidy -diff
```

---

#### `A-11` — `ctxSwitches` viaja siempre a cero y los errores de red se envían como acumulados, no como tasa

**Impacto en el usuario: Bajo · Facilidad: Fácil**

**Qué ocurre hoy.** Dos campos del contrato de ingesta que existen en las tres partes (el
tipo Go, el esquema del servidor y la documentación) pero cuyo valor no es el que se espera:

- **`metrics.cpu.ctxSwitches`.** Declarado en [`payload/payload.go:63`](../payload/payload.go)
  sin la etiqueta `omitempty`, y aceptado por el servidor en `schemas.py:142`. Ningún recolector
  lo rellena nunca, así que **siempre se envía el valor cero**. El servidor lo persiste como
  cero, y quien mire ese dato en la base de datos creerá que la máquina no hace ningún cambio
  de contexto.
- **`metrics.network[].errIn` y `errOut`.** [`collector/network.go:62-63`](../collector/network.go)
  los copia tal cual del contador de `gopsutil`, que es **acumulado desde el arranque de la
  máquina**. Los campos hermanos de la misma estructura (`rxBytesPerSec`, `txBytesPerSec`) sí se
  convierten a tasa por segundo, y el README §3 dice explícitamente que los errores deberían
  ser tasa. Un panel que muestre esos campos verá una recta siempre creciente, no un indicador
  de salud.

**Por qué es un problema.** Es información falsa presentada con la misma confianza que la
correcta. Un cero indistinguible de "no medido" es peor que un campo ausente, porque el
servidor ya está preparado para tratar los campos ausentes como "el agente no reportó esto"
(está documentado en `schemas.py:364`).

**Qué hacer.**

- Para `ctxSwitches`: o se rellena de verdad (`gopsutil/load.Misc()` lo expone en Linux; en
  Windows no hay equivalente directo) o se le pone `omitempty` para que desaparezca del JSON
  cuando no se mide. La segunda opción es la lazy y la honesta.
- Para `errIn`/`errOut`: aplicarles la misma función `rate()` que ya existe cinco líneas más
  arriba en el mismo fichero. Es un cambio de dos líneas. Hay que avisar al equipo del servidor
  porque cambia el significado de dos campos ya persistidos.

---

#### `A-12` — El inventario de Windows lee la rama de usuario del registro con la cuenta equivocada

**Impacto en el usuario: Medio · Facilidad: Media**

**Qué ocurre hoy.** [`collector/inventory_windows.go:29-33`](../collector/inventory_windows.go)
incluye entre las rutas a escanear:

```go
{
    registry.CURRENT_USER,
    `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
    "x64",
},
```

**Por qué es un problema.** `hygeia-agent` corre como servicio de Windows bajo la cuenta
`LocalSystem`. Para ese proceso, `HKEY_CURRENT_USER` **no** es la rama del usuario que ha
iniciado sesión en el escritorio: es la de `LocalSystem`, que está prácticamente vacía. En la
práctica, esa tercera ruta no aporta nada, y todo el software instalado "solo para este
usuario" —que en Windows moderno incluye buena parte de lo que instala un usuario sin
privilegios: navegadores, clientes de mensajería, herramientas de desarrollo— **no aparece en el
inventario**.

Es un problema de exactitud que se hereda hacia arriba: ese inventario alimenta el informe en
PDF del servidor y el análisis de vulnerabilidades con el motor Lybra. Un inventario incompleto
produce un análisis de vulnerabilidades incompleto que se presenta como completo.

**Qué hacer.** Enumerar `HKEY_USERS` y recorrer cada subclave que corresponda a un identificador
de seguridad de usuario real (los que empiezan por `S-1-5-21-` y no terminan en `_Classes`), en
lugar de usar `HKEY_CURRENT_USER`. Es exactamente lo que hacen los inventarios comerciales.
Como el servicio corre con privilegios elevados, tiene acceso a esa rama.

Es de facilidad "Media" y no "Fácil" porque conviene acompañarlo de deduplicación: el mismo
programa puede aparecer en la rama de máquina y en la de varios usuarios.

---

#### `A-13` — El README duplica el contrato de ingesta y esa copia ya está obsoleta

**Impacto en el usuario: Bajo · Facilidad: Muy fácil**

**Qué ocurre hoy.** El §9 del [`README.md`](../README.md) reproduce el contrato de ingesta
completo, con el argumento explícito de "que este repo sea autocontenido". El mismo contrato
está en el §11 del plan del backend (`EllysiaServer/plans/feature/hygeia/hygeia-backend.md`).

**Por qué es un problema.** Ya han divergido. El bloque JSON del README **no menciona el campo
`inventory`**, que existe en el código desde hace semanas, está en el tipo
`payload.Payload.Inventory`, está en el esquema del servidor y tiene una sección propia en la
configuración de límites. Cualquiera que use el README como referencia del contrato trabajará
con una versión incompleta. Y este análisis ha encontrado tres incompatibilidades reales entre
agente y servidor (`A-01`, `A-02`, `A-04`) precisamente en zonas que ninguno de los dos
documentos cubría con precisión.

**Qué hacer.** Ver `O-02`: un único documento de contrato, con su propia versión, referenciado
desde los dos repositorios en lugar de copiado en ambos. Es un problema de organización, y por
eso se desarrolla en la parte 2.

---

## 4. Parte 2 — Organización de ficheros

### 4.1. Veredicto general

**La estructura actual es correcta.** No hay nada que esté mal según las convenciones de Go, y
conviene decirlo con claridad porque la pregunta era justamente esa.

Lo que hay hoy es:

```
HygeiaAgent/
├── cmd/
│   ├── hygeia-agent/     paquete main del servicio
│   └── hygeia-tray/      paquete main del icono de bandeja
├── agent/                bucle principal
├── buffer/               ring buffer en disco
├── collector/            un fichero por familia de métricas
├── config/               carga de configuración y permisos
├── control/              canal local entre tray y servicio
├── payload/              tipos del contrato de ingesta
├── shipper/              envío HTTP
├── version/              versión inyectada en compilación
├── internal/
│   ├── autostart/        arranque con la sesión
│   ├── icon/             iconos de bandeja
│   └── logring/          últimas líneas de log en memoria
├── installer/            script de empaquetado para Windows
├── resources/            arte de marca
└── .github/workflows/    integración continua
```

Esto encaja con la convención de Go en todo lo importante:

- **`cmd/` para los binarios.** Es la convención universal cuando hay más de un ejecutable, y
  el propio README explica por qué apareció (§4).
- **Un paquete por responsabilidad, con nombre corto y en minúsculas.** `buffer`, `shipper`,
  `collector` son nombres correctos: describen lo que el paquete *es*, no lo que *hace*, y no
  caen en los antipatrones típicos (`utils`, `helpers`, `common`, `models`).
- **Ficheros por sistema operativo con sufijo.** `inventory_windows.go`, `perm_unix.go`,
  `transport_windows.go`, `autostart_darwin.go`. Es el mecanismo nativo de compilación
  condicional de Go, y es exactamente la forma correcta de resolverlo.
- **Pruebas junto al código que prueban**, en el mismo paquete. Correcto.
- **`internal/` para lo que no debe salir.** Bien usado, aunque de forma incompleta (ver `O-01`).

Dicho eso, hay tres mejoras que valen la pena. Ninguna es urgente; las tres ganan valor
conforme el repositorio crezca.

---

#### `O-01` — Mover los paquetes del núcleo bajo `internal/`

**Impacto en el usuario: Bajo · Facilidad: Fácil**

**Qué ocurre hoy.** Solo tres paquetes (`autostart`, `icon`, `logring`) están bajo `internal/`.
Los ocho del núcleo (`agent`, `buffer`, `collector`, `config`, `control`, `payload`, `shipper`,
`version`) están en la raíz, lo que en Go significa que son **importables desde fuera del
módulo**: cualquier proyecto podría escribir
`import "github.com/ProjectEllysia/Ellysia-Hygeia/buffer"`.

**Por qué merece la pena cambiarlo.** No es una cuestión estética. `internal/` es la única forma
que tiene Go de expresar "esto es detalle de implementación", y **el compilador la hace
cumplir**. Las consecuencias prácticas:

- **Libertad para refactorizar.** Mientras un paquete sea público, cambiar la firma de una
  función es teóricamente un cambio incompatible. Bajo `internal/`, se puede reorganizar lo que
  haga falta sin deber nada a nadie.
- **Claridad para quien llega.** Al abrir el repositorio, la raíz debería contestar "¿qué es
  esto y cómo se usa?". Hoy contesta con once directorios de implementación. Con la
  reorganización, la raíz muestra `cmd/`, `internal/`, `docs/`, `installer/`, `resources/`, y se
  entiende de un vistazo que es un proyecto de dos binarios.
- **Coste real, cero.** El repositorio no tiene consumidores externos. El cambio es mecánico:
  mover los directorios y reescribir las rutas de importación (`gofmt -r`, o simplemente buscar
  y reemplazar la cadena `Ellysia-Hygeia/` por `Ellysia-Hygeia/internal/`). El compilador
  detecta cualquier omisión.

**La única excepción a discutir: `payload/`.** Es el paquete que define el contrato con el
servidor. Hay un argumento para dejarlo público: si algún día se escribe una herramienta de
pruebas de carga, un simulador de agente o un cliente en Go, importar los tipos del contrato
sería lo natural. La recomendación es **moverlo también** —hoy no existe ese consumidor, y
sacarlo de `internal/` el día que exista cuesta cinco minutos— pero es una decisión legítima
en cualquiera de los dos sentidos.

**Estructura propuesta:**

```
HygeiaAgent/
├── cmd/
│   ├── hygeia-agent/
│   └── hygeia-tray/
├── internal/
│   ├── agent/
│   ├── autostart/
│   ├── buffer/
│   ├── collector/
│   ├── config/
│   ├── control/
│   ├── icon/
│   ├── logring/
│   ├── payload/
│   ├── shipper/
│   └── version/
├── docs/
├── installer/
├── resources/
├── .github/workflows/
├── go.mod
├── README.md
└── VERSION.txt
```

---

#### `O-02` — Partir el README en tres documentos con propósitos distintos

**Impacto en el usuario: Bajo · Facilidad: Fácil**

**Qué ocurre hoy.** El [`README.md`](../README.md) tiene doscientas noventa y siete líneas y
hace tres trabajos a la vez:

1. **Manual de uso** (§0): cómo compilar, cómo instalar el servicio, cómo usar el tray.
2. **Documento de diseño** (§1 a §8): la filosofía, por qué Go y no Python, qué métricas se
   recogen, las fases de entrega, incluida una tabla comparativa de lenguajes que era una
   decisión a tomar en su día y hoy es historia.
3. **Copia del contrato de ingesta** (§9), duplicado del §11 del plan del backend.

**Por qué merece la pena cambiarlo.** Cada uno de los tres tiene un público y un ritmo de
cambio distintos, y mezclarlos tiene consecuencias medibles:

- Quien solo quiere instalar el agente tiene que atravesar una tabla que compara Go con Rust
  para llegar a los comandos.
- El §7 ("Fases") es una tabla de estado que ya está desactualizada: marca la fase 4 como
  pendiente, pero no menciona el inventario de software, que se implementó después y no encaja
  en ninguna de las fases listadas.
- La copia del contrato **ya divergió** (`A-13`): no menciona el campo `inventory`. Dos copias
  de un contrato son dos contratos.
- Los enlaces mienten: el README enlaza a [`docs/GUIA-GO.md`](../docs/GUIA-GO.md) dos veces
  (líneas 98 y 99) y **ese fichero no existe**. El directorio `docs/` no existía en absoluto
  antes de este documento.

**Qué hacer.**

| Documento | Contenido | Público |
|---|---|---|
| `README.md` | Qué es Hygeia en un párrafo, cómo compilar, cómo instalar el servicio y el tray, la matriz de plataformas, y enlaces al resto. Objetivo: menos de cien líneas. | Cualquiera que llegue al repositorio |
| `docs/DISENO.md` | La filosofía del §1, la arquitectura interna del §4, las decisiones de resiliencia y seguridad del §5, y las fases del §7 puestas al día. La comparativa de lenguajes del §2 se conserva aquí como registro histórico de la decisión. | Quien vaya a modificar el agente |
| `docs/CONTRATO-INGESTA.md` | El contrato completo, con su propia versión (`v1.1`, por ejemplo) y una tabla de cambios. **Fuente única.** El plan del backend y el README enlazan aquí en lugar de copiarlo. | Ambos repositorios |

Sobre dónde vive el contrato: como es la costura entre dos repositorios, lo ideal sería un
único fichero referenciado por ambos. Si no hay un sitio compartido, la regla práctica es
**declarar cuál manda** — la recomendación es que mande el del agente, porque es quien produce
los datos, y que el backend enlace a él — y **poner una prueba automática que lo verifique**:
un fichero JSON de ejemplo del contrato en el repositorio del agente, que las pruebas del
servidor validen contra su esquema Marshmallow. Esa única prueba habría detectado `A-01` el
primer día.

Y aprovechando: escribir el `docs/GUIA-GO.md` que el README promete, o quitar los dos enlaces.

---

#### `O-03` — Reforzar la integración continua y añadir la publicación de versiones

**Impacto en el usuario: Medio · Facilidad: Media**

**Qué ocurre hoy.** [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) es un único
trabajo que, en los tres sistemas operativos, ejecuta `go vet`, `go build` y `go test`. Está
bien montado: matriz de plataformas, cancelación de ejecuciones anteriores, y una nota que
explica el problema del enlazador de Xcode con comentarios de calidad.

Le faltan cinco cosas:

1. **Formato.** No hay comprobación de `gofmt`. Y hace falta: la estructura `payload.Software`
   en [`payload/payload.go:36-48`](../payload/payload.go) tiene la alineación de campos rota
   (mezcla de tabulaciones que `gofmt` corregiría). Es cosmético, pero es la clase de cosa que
   ensucia todos los `diff` posteriores.
2. **Análisis estático.** No hay `golangci-lint`. Con la configuración por defecto habría
   detectado, entre otras cosas, errores no comprobados y comparaciones de errores con `==` en
   lugar de `errors.Is` ([`cmd/hygeia-tray/main.go:269`](../cmd/hygeia-tray/main.go) compara
   `err != zenity.ErrCanceled`).
3. **Coherencia de módulos.** `go mod tidy -diff` habría detectado `A-10` inmediatamente.
4. **Vulnerabilidades en dependencias.** `govulncheck ./...` es un paso de treinta segundos que
   avisa de CVEs conocidos en las dependencias. En un producto de seguridad, no tenerlo es
   difícil de defender.
5. **Publicación de versiones.** No hay ningún flujo que, al etiquetar una versión, compile la
   matriz completa de binarios (`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`,
   `darwin/arm64`), calcule sus sumas de verificación, los firme y los publique como una
   release de GitHub. Esto no es solo higiene: **es un requisito de dos cosas ya planificadas**
   — las "releases firmadas" que el README §7 marca como pendientes de la fase 3, y la sección
   de descarga del agente desde Ellysia (§15 del plan del backend, fase `D0`), que asume
   explícitamente que existen binarios precompilados y firmados que servir.

**Qué hacer.** Añadir los cuatro pasos de verificación al flujo existente, y crear un segundo
flujo `release.yml` disparado por etiquetas `v*`. Se detalla como funcionalidad en `F-08`,
porque el valor para el usuario está ahí.

---

#### `O-04` — Detalles menores de organización

**Impacto en el usuario: Bajo · Facilidad: Muy fácil**

Un grupo de cosas pequeñas que se pueden resolver en una sola sesión:

- **Dos fuentes de verdad para la versión.** [`VERSION.txt`](../VERSION.txt) dice `1.0.4`;
  [`version/version.go`](../version/version.go) tiene como valor por defecto `0.1.0-dev`. El
  script del instalador lee el primero y lo inyecta por `-ldflags`, pero un binario compilado a
  mano con `go build` reporta `0.1.0-dev` al backend, que lo persiste en
  `MonitoredAsset.agent_version`. Conviene documentar en el README que compilar sin `-ldflags`
  produce una versión de desarrollo, o hacer que `version.go` lea `VERSION.txt` con `go:embed`
  para que haya una sola fuente.
- **Arte duplicado.** `resources/hygeia/Hygeia-DarkGreen-BgN.png` es el original; su versión
  recortada y reescalada vive en `internal/icon/hygeia-mark.png`, y el generador
  `internal/icon/gen/main.go` produce la segunda a partir de la primera. Es una decisión
  correcta y está explicada (`go:embed` no puede salir del directorio del paquete), pero falta
  la directiva que hace explícita la relación. Añadir en `internal/icon/icon.go`:

  ```go
  //go:generate go run ./gen
  ```

  Así `go generate ./...` regenera el arte y nadie tiene que recordar el comando.
- **Ficheros de trabajo en la raíz.** `hygeia-agent.exe`, `hygeia-agent.exe~`,
  `hygeia-tray.exe`, `hygeia-tray.exe~`, `hygeia-buffer.jsonl` y `config.toml` están en el
  directorio de trabajo. **Todos están correctamente ignorados por Git** —lo he verificado con
  `git ls-files`, ninguno está versionado— así que no es un problema del repositorio, solo
  desorden local. Un `make clean` o una nota en el README bastaría.
- **Faltan ficheros estándar del repositorio.** No hay `LICENSE`, ni `CHANGELOG.md`, ni
  `CONTRIBUTING.md`, ni configuración de Dependabot. Para un proyecto que se va a distribuir
  como binario a clientes, el `LICENSE` y el `CHANGELOG` son los que de verdad se echan en
  falta.
- **Un `return` muerto en el servidor.** De paso, y aunque cae fuera de este repositorio:
  `EllysiaServer/API/src/modules/features/hygeia/endpoints.py:367` tiene un `return` después de
  otro `return`, código inalcanzable que quedó de un copiar y pegar. Inofensivo, pero conviene
  quitarlo.

---

## 5. Parte 3 — Funcionalidades nuevas

Para esta parte he revisado el módulo `hygeia` del servidor
(`EllysiaServer/API/src/modules/features/hygeia`), sus dos planes de diseño
(`plans/feature/hygeia/hygeia-backend.md` y `plans/feature/hygeia/acheron-hygeia-agent-key-vault.md`)
y los componentes de la interfaz web (`web/app/src/components/hygeia/`).

La conclusión más útil de esa revisión: **el servidor está por delante del agente**. Hay
capacidades ya construidas y desplegadas en el backend que el agente no aprovecha, y esa es la
lista con mejor relación entre valor entregado y esfuerzo.

---

### 5.1. Cerrar lo que el servidor ya sabe hacer

---

#### `F-01` — Inventario de software en Linux y macOS

**Impacto en el usuario: Alto · Facilidad: Media**

**Situación actual.** [`collector/inventory_linux.go`](../collector/inventory_linux.go) y
[`collector/inventory_darwin.go`](../collector/inventory_darwin.go) son funciones vacías de
siete líneas que devuelven un inventario vacío sin error. Solo Windows tiene implementación
real.

**Qué hay ya construido al otro lado.** Absolutamente toda la tubería:

- El servidor persiste el inventario en `MonitoredAsset.inventory` (`managers.py:790`).
- `GET /hygeia/assets/{id}/inventory` lo devuelve.
- `POST /hygeia/inventory/report` genera un informe en PDF con anexo de software
  (`services/reports.py`, quinientas líneas ya escritas).
- `POST /hygeia/assets/{id}/analyze` traduce el inventario a servicios y lanza un análisis de
  vulnerabilidades con el motor Lybra (`services/inventory_adapter.py`), con el resultado
  visible en la interfaz (`web/app/src/components/hygeia/InventoryAnalysisModal.vue`).

Es decir: **un activo Linux con el agente instalado hoy aparece en Ellysia como una máquina sin
ningún software instalado**, y su informe de inventario sale vacío. No es que falte una
funcionalidad; es que una funcionalidad existente da un resultado incorrecto en dos de los tres
sistemas operativos soportados.

**Qué hacer.**

- **Linux:** leer `/var/lib/dpkg/status` en distribuciones basadas en Debian y la base de datos
  RPM en las basadas en Red Hat. El `go.mod` ya declaraba `go-dpkg` y `go-rpmdb` con esta
  intención (ver `A-10`). Merece la pena valorar la alternativa perezosa: leer directamente
  `/var/lib/dpkg/status`, que es un fichero de texto con formato de cabeceras estilo correo
  electrónico y se analiza en unas cuarenta líneas sin ninguna dependencia. Para RPM sí conviene
  la librería, porque la base de datos es binaria. Complementar con `flatpak list` y `snap list`
  si están presentes.
- **macOS:** recorrer `/Applications` y `/Applications/Utilities` leyendo el `Info.plist` de
  cada paquete `.app` (nombre, versión, identificador). Complementar con `brew list --versions`
  si Homebrew está instalado.
- En ambos casos, rellenar los campos que el esquema del servidor ya acepta: `source` (con
  valores como `dpkg`, `rpm`, `brew`, `plist`) y `architecture`, para que el informe pueda
  distinguir el origen de cada entrada.

---

#### `F-02` — Dar de alta el agente desde la línea de comandos, sin icono de bandeja

**Impacto en el usuario: Alto · Facilidad: Muy fácil**

**Situación actual.** Hay exactamente dos formas de darle su clave a un agente recién
instalado: usar el icono de bandeja (`hygeia-tray`), o editar a mano el fichero `config.toml`
en `C:\ProgramData\Hygeia\` o `/etc/hygeia/`, con privilegios de administrador, y reiniciar el
servicio.

**Por qué es un problema.** El README §11.6 dice explícitamente que "un servidor headless corre
`hygeia-agent` solo, sin tray". Pero es justamente en un servidor sin escritorio donde no hay
tray, y donde editar TOML por SSH es el único camino. Se ha construido un canal de control
local completo, con validación de la clave, respuestas de error claras y control de permisos —
y el único cliente de ese canal es una interfaz gráfica.

**Qué hacer.** Añadir un subcomando al binario del agente:

```bash
hygeia-agent enroll <clave>     # entrega la clave al servicio en marcha
hygeia-agent reset              # borra la clave local
hygeia-agent info               # estado del agente: conectado, buffer, último envío
```

La implementación es de una tarde: [`control/client.go`](../control/client.go) ya tiene los
métodos `Enroll`, `Reset` y `Status`; solo hay que añadir tres `case` al `switch` de
[`cmd/hygeia-agent/commands.go:16`](../cmd/hygeia-agent/commands.go), exactamente igual que el
`case "debug"` que ya llama a `runDebug()`. El servidor no necesita ningún cambio.

Es la mejor relación esfuerzo/beneficio de toda la lista, y desbloquea el despliegue
automatizado: con este subcomando, un script de Ansible o un `cloud-init` puede instalar y dar
de alta un agente sin intervención humana.

---

#### `F-03` — Gestionar la rotación y la revocación de claves

**Impacto en el usuario: Alto · Facilidad: Media**

**Situación actual.** El servidor tiene `POST /hygeia/assets/{id}/rotate-key`
(`endpoints.py:370`), que genera una clave nueva e **invalida la anterior de inmediato**. Desde
ese momento, el agente recibe `401` en cada envío.

El agente no gestiona ese caso de ninguna manera especial:

- `401` no está en `isPermanentStatus` ([`shipper/shipper.go:79`](../shipper/shipper.go)), y con
  buen criterio: el comentario explica que tras un reset y un alta nueva el mismo payload sí
  podría entregarse.
- Pero eso significa que el agente **reintenta cuatro veces con espera exponencial en cada
  ciclo** y va llenando el buffer con payloads que nunca se aceptarán, hasta desbordarlo.
- El estado que ve el usuario es `local_error` con el mensaje "backend devolvió status 401", que
  no explica qué ha pasado ni qué hacer.
- Para recuperarse hace falta: abrir el tray, pulsar "Reestablecer configuración", confirmar,
  pulsar "Introducir clave de agente", pegar la nueva. Cinco pasos.

**Qué hacer.** Tres cambios que se refuerzan entre sí:

1. **Un estado propio para "clave rechazada".** Añadir `StateKeyRejected` a
   [`control/control.go`](../control/control.go), junto a los cuatro que ya existen. El tray
   muestra un icono distinto y un texto que dice qué pasa: "la clave ya no es válida; pide una
   nueva en Ellysia".
2. **No llenar el buffer con lo que no se puede entregar.** Con `401` sostenido, dejar de
   guardar payloads nuevos: no se van a poder enviar, y ocupan el sitio de los que sí se
   podrían.
3. **Permitir sustituir la clave sin reset previo.** Hoy [`agent/agent.go:97`](../agent/agent.go)
   rechaza cualquier `Enroll` sobre un agente ya configurado, por una razón de seguridad válida
   (que un usuario local sin privilegios no pueda reapuntar el agente). La forma de conservar
   esa garantía y aun así permitir la rotación es aceptar el `Enroll` **cuando el estado sea
   `key_rejected`**: si el servidor ya ha rechazado la clave actual, no hay nada que proteger.

---

#### `F-04` — Un subcomando de diagnóstico para soporte

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Situación actual.** Cuando un agente no reporta, las herramientas disponibles son
`hygeia-agent status` (que solo dice si el servicio del sistema operativo está en marcha) y
`hygeia-agent debug` (que muestra goroutines, memoria y las últimas cincuenta líneas de log).
Ninguno responde a la pregunta que de verdad se hace quien depura: *¿por qué no llega?*

**Qué hacer.** Un subcomando `hygeia-agent doctor` que verifique, en orden, y con un resultado
legible por línea:

```
hygeia-agent doctor

  [ok]    Fichero de configuración   C:\ProgramData\Hygeia\config.toml
  [ok]    Permisos del fichero        solo SYSTEM y Administradores
  [ok]    Formato de la clave         keyId.secreto (válido)
  [ok]    Resolución DNS              ellysia.ejemplo.com -> 203.0.113.10
  [ok]    Conectividad TCP            203.0.113.10:443 alcanzable (24 ms)
  [ok]    Certificado TLS             válido, caduca el 2027-01-14
  [fallo] Autenticación               el servidor respondió 401
          -> La clave ha sido revocada o rotada. Pide una nueva en Ellysia
             y aplícala con: hygeia-agent enroll <clave>
  [aviso] Desviación de reloj         el reloj local va 412 s por delante del servidor
          -> El servidor rechaza heartbeats con más de 300 s de desviación.
             Sincroniza el reloj del sistema.
  [ok]    Buffer                      0 payloads pendientes
```

Cada comprobación es de tres a diez líneas de código y todas usan piezas que ya existen. Reduce
drásticamente el tiempo de resolución de una incidencia, y como efecto secundario documenta el
modelo de fallo del sistema: cada comprobación es una hipótesis de por qué un agente se queda
mudo. La comprobación de desviación de reloj es especialmente valiosa, porque hoy ese fallo se
manifiesta como un `400` genérico y opaco.

---

#### `F-05` — Enviar solo los inventarios que han cambiado

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Situación actual.** Cada seis horas, el agente escanea el software instalado y envía la lista
completa, aunque no haya cambiado nada. En una máquina con dos mil aplicaciones son varios
cientos de kilobytes por envío, cuatro veces al día, por cada equipo de la flota. El servidor
reemplaza el inventario completo cada vez (`managers.py:790`).

Esto es exactamente la **fase H3** del plan del backend, que quedó descrita pero no
implementada.

**Qué hacer.** El agente calcula un hash (por ejemplo SHA-256) del inventario ordenado y lo
guarda. Si el hash del escaneo nuevo coincide con el guardado, no adjunta el inventario al
heartbeat. El campo ya es opcional (`inventory *Inventory` con `omitempty`) y el servidor ya
interpreta su ausencia como "el agente no escaneó en este heartbeat, conserva el anterior" —
está documentado explícitamente en `managers.py:786`. **No hace falta ningún cambio en el
servidor.**

Conviene añadir un envío forzado periódico (por ejemplo una vez al día aunque el hash no cambie)
para que un inventario perdido por un fallo del servidor acabe reconciliándose solo.

---

### 5.2. Ampliar lo que el agente observa

---

#### `F-06` — Señales de seguridad (la fase 4 pendiente)

**Impacto en el usuario: Alto · Facilidad: Media a Difícil**

**Situación actual.** El README §3 describe tres señales de seguridad como "el diferencial de
Ellysia", y el §7 las marca como fase 4 no implementada. El campo `localAlerts` **existe en el
contrato de ingesta, existe en el tipo Go, y el servidor ya lo acepta** (`schemas.py:276`), pero
el agente nunca lo rellena.

**Las tres señales, con su viabilidad real:**

| Señal | Cómo se obtiene | Facilidad |
|---|---|---|
| **Puertos nuevos a la escucha** | `gopsutil/net.Connections("inet")` con estado `LISTEN`; comparar con el conjunto del ciclo anterior. La librería ya está en el proyecto. | **Fácil** |
| **Proceso sospechoso con CPU alta** | Ya se tiene el ejecutable de cada proceso del top; comprobar si su ruta está en `/tmp`, `/dev/shm`, `%TEMP%` y lleva varios ciclos con CPU alta. | **Fácil** |
| **Recuento de inicios de sesión fallidos** | Requiere leer el registro de eventos de seguridad de Windows (evento 4625) y `/var/log/auth.log` o el diario de systemd en Linux. Tres implementaciones distintas, con problemas de permisos y de formato en cada una. | **Difícil** |

**Qué hacer.** Empezar por las dos primeras, que son fáciles y ya cubren los dos escenarios que
el plan del backend cita como motivación ("un pico sostenido de CPU por un proceso desconocido
huele a cryptominer; un puerto nuevo a la escucha, a persistencia"). Dejar la tercera para
después, y en ese momento valorar si compensa el coste.

Una observación importante de diseño: el README §1 insiste, con razón, en que "la autoridad de
detección vive en el backend". Enviar `localAlerts` **no viola ese principio**, porque el
formato del campo es `{tipo, mensaje}` — el agente reporta un hecho observado ("apareció un
puerto a la escucha en el 4444"), no un veredicto. La decisión de si eso es una anomalía sigue
siendo del servidor.

Eso sí: hoy el servidor **acepta y descarta** ese campo (`fields.List(fields.Raw())`, sin
procesarlo). Para que las señales sirvan de algo hace falta trabajo en el servidor: persistirlas
y convertirlas en anomalías. Por eso el rango de facilidad va de "Media" a "Difícil": el lado
del agente es fácil, cerrar el círculo no.

---

#### `F-07` — Ampliar el contrato con métricas que el README promete y no se envían

**Impacto en el usuario: Medio · Facilidad: Media**

**Situación actual.** El README §3 enumera varias métricas que no llegan a viajar en el payload:

| Métrica prometida | Estado real | Coste de añadirla |
|---|---|---|
| Tasas de entrada/salida de disco e IOPS | No implementada. `gopsutil/disk.IOCounters` la da y solo hay que aplicar el mismo patrón de diferencia entre ciclos que usa la red. | Bajo |
| Número de conexiones de red activas | No implementada. `gopsutil/net.Connections`. Se solapa con `F-06`. | Bajo |
| Temperatura de sensores | No implementada. `gopsutil/host.SensorsTemperatures`. Poco fiable en muchas máquinas virtuales y en Windows sin drivers. | Bajo |
| Usuarios conectados | No implementada. `gopsutil/host.Users`. | Muy bajo |
| Cambios de contexto | Campo presente pero siempre a cero (ver `A-11`). | Bajo |

**Por qué la facilidad es "Media" y no "Fácil".** El coste no está en el agente: cada una de
estas métricas son diez o quince líneas usando una librería que ya está. Está en que **cada una
requiere ampliar el esquema del servidor, la migración de base de datos, y la interfaz web** para
que el dato sirva de algo. Enviar datos que nadie muestra es coste sin beneficio.

**Qué hacer.** Tratarlo como una única ampliación coordinada del contrato (versión `1.1`) en
lugar de cinco cambios sueltos, y decidir antes qué se va a mostrar en la interfaz. La
recomendación de prioridad, por valor real de diagnóstico: entrada/salida de disco primero (un
disco saturado es una causa muy común de lentitud y hoy es invisible), usuarios conectados
después (barato y útil para el inventario), y las demás cuando haya demanda.

---

### 5.3. Distribución e instalación

---

#### `F-08` — Publicación automática de binarios firmados para las cinco plataformas

**Impacto en el usuario: Alto · Facilidad: Media**

**Situación actual.** Los binarios se compilan a mano. El único empaquetado que existe es
[`installer/build-installer.ps1`](../installer/build-installer.ps1), un script de PowerShell
que compila los dos ejecutables de Windows y genera un instalador de Inno Setup — está bien
hecho, pero solo cubre Windows y solo se ejecuta manualmente desde una máquina de desarrollo.

No hay binarios para Linux ni para macOS, no hay sumas de verificación publicadas, no hay firma.
El README §7 marca "releases firmadas" como pendiente de la fase 3.

**Por qué importa.** Tres razones que se acumulan:

1. **Es un requisito de una funcionalidad ya planificada.** La sección de descarga del agente
   desde Ellysia (§15 del plan del backend, fase `D0`) asume explícitamente que "en su CI se
   genera, en cada release, una matriz de binarios firmados". Sin eso, la fase `D0` no se puede
   construir.
2. **Es un producto de seguridad.** Distribuir un ejecutable sin firma ni suma de verificación,
   que además se instala como servicio con privilegios de sistema, es difícil de justificar ante
   un cliente. En Windows, un binario sin firmar dispara la advertencia de SmartScreen, que es
   exactamente la fricción que un instalador de doble clic pretendía evitar.
3. **El agente no existe fuera de Windows en la práctica.** El código compila en los tres
   sistemas —la integración continua lo demuestra— pero nadie fuera del equipo puede obtener un
   binario de Linux.

**Qué hacer.** Un flujo `release.yml` disparado por etiquetas `v*` que:

- Compile la matriz `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`,
  `darwin/arm64`, inyectando la versión con `-ldflags` desde la etiqueta.
- Genere un fichero `checksums.txt` con los SHA-256.
- Firme los binarios de Windows (certificado de firma de código) y de macOS (`codesign` más
  notarización de Apple, que es el paso más laborioso).
- Ejecute el script del instalador de Windows y adjunte el `.exe` resultante.
- Publique todo como una release de GitHub.
- Idealmente, genere también paquetes `.deb` y `.rpm` con la unidad de systemd ya incluida, para
  que instalar en Linux sea `apt install` en lugar de "copia este binario y ejecuta
  `hygeia-agent install`".

La facilidad es "Media" y el grueso del trabajo no es el flujo en sí (que es media jornada con
`goreleaser`), sino conseguir y custodiar los certificados de firma.

---

#### `F-09` — Configuración para entornos corporativos: proxy y autoridades de certificación propias

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Situación actual.** [`shipper/shipper.go:44`](../shipper/shipper.go) crea el cliente HTTP así:

```go
client: &http.Client{Timeout: 15 * time.Second},
```

Al no indicar un `Transport`, se usa `http.DefaultTransport`, que **sí respeta** las variables
de entorno `HTTP_PROXY`, `HTTPS_PROXY` y `NO_PROXY`. Eso ya es algo. Pero:

- Un servicio de Windows corriendo como `LocalSystem` **no hereda** las variables de entorno del
  usuario, ni la configuración de proxy del navegador. Habría que definirlas a nivel de máquina.
- No hay forma de configurar un proxy con autenticación desde el `config.toml`.
- No hay forma de añadir una autoridad de certificación propia. En una red corporativa con
  inspección TLS —que es lo normal en empresas medianas y grandes— el certificado que ve el
  agente está emitido por la autoridad interna, y como el README §5 exige (con razón) rechazar
  certificados inválidos, **el agente no puede conectar en absoluto**.

**Por qué importa.** Es un bloqueo total, no una degradación: en una red con inspección TLS, el
producto no funciona. Y el diagnóstico es difícil, porque el síntoma es un error de certificado
genérico.

**Qué hacer.** Tres campos nuevos en la configuración, todos opcionales:

```toml
proxyUrl = "http://proxy.empresa.local:3128"   # con credenciales si hace falta
caFile   = "C:/ProgramData/Hygeia/empresa-ca.pem"   # autoridad adicional, NO sustituye al almacén del sistema
```

La implementación es un `http.Transport` explícito con `Proxy` y un `tls.Config` cuyo
`RootCAs` sea una copia del almacén del sistema **con** el certificado adicional añadido. Es
importante que sea aditivo y no sustitutivo, para no debilitar la validación. Unas cuarenta
líneas. Y `F-04` (el subcomando `doctor`) debería comprobar precisamente esto.

---

#### `F-10` — Ajuste remoto de la configuración del agente

**Impacto en el usuario: Medio · Facilidad: Difícil**

**Situación actual.** El servidor ya ajusta una cosa a distancia: devuelve `nextIntervalSec` en
cada respuesta de ingesta y el agente lo aplica sin reiniciarse
([`agent/agent.go:284`](../agent/agent.go)). El mecanismo funciona y está probado. Pero es lo
único: cambiar qué recolectores están activos, cada cuánto se escanea el inventario o cuántos
procesos entran en el top exige tocar el `config.toml` de cada equipo.

**Qué hacer.** Extender la respuesta de ingesta con un bloque de configuración opcional:

```jsonc
{
  "ok": true,
  "nextIntervalSec": 15,
  "serverTime": "2026-08-12T10:00:01Z",
  "config": {                          // opcional, solo cuando el servidor quiere cambiar algo
    "collectors": ["cpu", "memory", "disk"],
    "inventoryIntervalSec": 86400,
    "topProcesses": 10
  }
}
```

Con dos reglas de seguridad que no son negociables: **el servidor nunca puede modificar
`serverUrl` ni `agentKey`** (eso permitiría a un backend comprometido secuestrar toda la flota),
y todos los valores recibidos se validan contra los mismos límites que ya aplica
`config.Load()`.

Se cataloga como "Difícil" porque requiere trabajo coordinado en ambos repositorios: modelo de
datos, interfaz de administración en la web, y ampliación del contrato. El valor es real —
gestionar una flota de doscientos equipos editando doscientos ficheros TOML no escala — pero no
es urgente hasta que la flota sea grande.

---

#### `F-11` — Actualización automática del agente

**Impacto en el usuario: Medio · Facilidad: Difícil**

**Situación actual.** No existe. El README §5 la menciona como "opcional y de nicho — no en
beta", y esa valoración sigue siendo correcta.

**Qué haría falta.** Depende por completo de `F-08` (no se puede actualizar automáticamente a un
binario que no se publica de forma verificable) y arrastra una lista larga de problemas
delicados: verificación de firma antes de ejecutar, sustitución de un binario que está en uso
—en Windows hay que renombrar y reiniciar el servicio—, reversión si la versión nueva no
arranca, despliegue escalonado para no romper la flota entera con una versión defectuosa, y una
política de qué versiones puede saltar.

**Recomendación: no hacerlo todavía.** Es la funcionalidad con peor relación entre riesgo y
valor de toda la lista. Un agente que se actualiza solo y se rompe deja la flota entera ciega, y
la vía de recuperación es visitar cada equipo. Mientras la flota sea manejable, actualizar por
las herramientas de despliegue que el cliente ya tenga (WSUS, Intune, Ansible, `apt`) es más
seguro y no cuesta nada construirlo. Se menciona aquí para dejar constancia de que se ha
valorado y descartado conscientemente, no por olvido.

---

#### `F-12` — Reportar direcciones de red para reconciliar identidad con Themis

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Situación actual.** El bloque `host` del payload lleva `hostname`, `os`, `kernel` y
`uptimeSec`. No lleva direcciones IP ni direcciones MAC.

**Por qué importa.** El plan del backend, en su fase `H1`, describe el problema de que un mismo
equipo físico aparezca como dos entidades distintas: un `Host` de Themis descubierto por
dirección IP con Nmap, y un `MonitoredAsset` de Hygeia identificado por nombre. La fase se
aplazó ("H1 queda diferido a propósito") porque el motor Lybra resuelve el host por nombre y no
lo necesitaba. Pero la nota deja claro que sigue haciendo falta "para fundir por `dedup_key` un
mismo host físico visto por IP (Nmap) y por hostname (Hygeia)".

Con las direcciones IP y MAC en el payload, esa reconciliación pasa a ser posible: la MAC es un
identificador estable y la IP es el punto de unión directo con lo que Nmap ve.

**Qué hacer.** Añadir al bloque `host` una lista de interfaces con su nombre, sus direcciones IP
y su dirección MAC, filtrando las de bucle invertido. Son unas quince líneas con
`gopsutil/net.Interfaces`, que ya es una dependencia. El valor completo depende de que el
servidor implemente la reconciliación, pero **el dato hay que empezar a recogerlo antes** para
que cuando se implemente ya haya histórico.

---

#### `F-13` — Etiquetas de identidad del equipo para el inventario

**Impacto en el usuario: Medio · Facilidad: Fácil**

**Situación actual.** El modelo `MonitoredAsset` del servidor tiene un campo `labels` de tipo
JSONB pensado para "etiquetas libres: entorno, rol, ubicación", y la interfaz web tiene un
sistema completo de etiquetas con colores (`TagBadge.vue`, `AssetTagsModal.vue`). Pero las
etiquetas solo se pueden poner **a mano**, una por una, desde la interfaz.

Mientras tanto, el agente está dentro del equipo y tiene acceso directo a datos de identidad
que hoy no envía: fabricante y modelo, número de serie, si está unido a un dominio y a cuál,
si es una máquina virtual y de qué hipervisor, edición exacta del sistema operativo.

**Qué hacer.** Añadir un bloque opcional al payload con esos datos y dejar que el servidor los
convierta en etiquetas automáticas. `gopsutil/host.Info()` ya devuelve parte (`platform`,
`platformVersion`, `virtualizationSystem`); el resto sale de WMI en Windows y de
`/sys/class/dmi/id/` en Linux.

El beneficio para el usuario es concreto: poder filtrar el listado de activos por "todos los
portátiles Dell del dominio corporativo" o "todas las máquinas virtuales de VMware" sin haber
etiquetado nada a mano, y que el informe en PDF de inventario incluya esa información.

---

## 6. Parte 4 — Catálogo completo ordenado

Ordenado por prioridad, entendida como impacto alto combinado con facilidad alta.

| ID | Título | Impacto | Facilidad | Fase |
|---|---|---|---|---|
| `A-01` | Acotar el porcentaje de CPU por proceso | **Crítico** | Muy fácil | 1 |
| `A-04` | Acotar el inventario y no consumirlo antes de confirmar el envío | Alto | Muy fácil | 1 |
| `A-02` | Alinear el buffer con la ventana de reloj del servidor | **Crítico** | Fácil | 1 |
| `F-02` | Alta y diagnóstico por línea de comandos | Alto | Muy fácil | 1 |
| `A-07` | Buffer en disco de coste lineal (empezando por `Len`) | Alto | Media | 2 |
| `A-05` | Recolector de procesos sin llamadas redundantes | Alto | Fácil | 2 |
| `A-03` | Drenado acotado y `429` tratado como cadencia | Alto | Media | 2 |
| `A-09` | Log acotado y menos ruidoso | Medio | Fácil | 2 |
| `A-06` | CPU sin bloqueo de un segundo | Medio | Fácil | 2 |
| `A-10` | Arreglar `go.mod` y verificarlo en integración continua | Bajo | Muy fácil | 3 |
| `O-03` | Formato, análisis estático y vulnerabilidades en integración continua | Medio | Media | 3 |
| `O-01` | Mover los paquetes del núcleo bajo `internal/` | Bajo | Fácil | 3 |
| `O-02` | Partir el README en tres documentos | Bajo | Fácil | 3 |
| `A-13` | Contrato de ingesta con fuente única | Bajo | Muy fácil | 3 |
| `O-04` | Detalles menores de organización | Bajo | Muy fácil | 3 |
| `F-03` | Rotación y revocación de claves | Alto | Media | 4 |
| `F-04` | Subcomando `doctor` | Medio | Fácil | 4 |
| `F-09` | Proxy y autoridades de certificación propias | Medio | Fácil | 4 |
| `F-01` | Inventario en Linux y macOS | Alto | Media | 5 |
| `A-12` | Inventario de Windows: rama de usuario correcta | Medio | Media | 5 |
| `F-05` | Envío diferencial del inventario | Medio | Fácil | 5 |
| `F-08` | Publicación automática de binarios firmados | Alto | Media | 6 |
| `F-06` | Señales de seguridad | Alto | Media a Difícil | 7 |
| `F-12` | Direcciones de red en el payload | Medio | Fácil | 7 |
| `F-13` | Etiquetas de identidad del equipo | Medio | Fácil | 7 |
| `A-11` | `ctxSwitches` y errores de red como tasa | Bajo | Fácil | 8 |
| `F-07` | Métricas nuevas del contrato | Medio | Media | 8 |
| `A-08` | Top-5 sin ordenar la lista entera | Bajo | Muy fácil | 8 |
| `F-10` | Configuración remota del agente | Medio | Difícil | 9 |
| `F-11` | Actualización automática | Medio | Difícil | Aplazada |

---

## 7. Parte 5 — Agrupación en fases

El criterio para agrupar no es solo la prioridad, sino **qué cosas conviene hacer juntas**:
porque tocan el mismo fichero, porque comparten pruebas, porque una necesita a la otra, o
porque juntas cierran una historia completa para el usuario.

---

### Fase 1 — Que los datos lleguen

> **`A-01`, `A-02`, `A-04`, `F-02`**
> Esfuerzo estimado: 3 a 4 días · Impacto: **Crítico**

**Por qué van juntas.** Las tres primeras son la misma historia: el agente cree que envía y el
servidor descarta. Las tres se resuelven en la costura entre `payload`, `shipper` y `agent`, y
las tres comparten la misma prueba de integración —un servidor de mentira que aplique las
mismas validaciones que el real— que solo merece la pena escribir una vez. `F-02` entra aquí
porque es de una tarde, porque no depende de nada, y porque es lo que permite desplegar en
servidores Linux para **verificar** que las tres correcciones funcionan.

**Qué se hace.**
1. Acotar `cpuPct` por proceso, decidiendo antes si se normaliza por número de núcleos (en el
   agente) o se amplía el rango (en el servidor). `A-01`
2. Acotar el número de elementos del inventario y mover su limpieza a después de un envío
   confirmado. `A-04`
3. Decidir y aplicar la política del buffer: ampliar la ventana de reloj del servidor (opción
   recomendada) o reducir la capacidad del buffer, y hacer esa capacidad configurable. `A-02`
4. Añadir los subcomandos `enroll`, `reset` e `info` al binario del agente. `F-02`
5. Escribir una prueba de integración con un servidor de mentira que replique las validaciones
   del esquema real, y un fichero JSON de ejemplo del contrato que las pruebas del servidor
   puedan validar.

**Cómo se sabe que está hecho.** Un equipo con un proceso saturando cuatro núcleos reporta sin
interrupción; un corte de backend de treinta minutos se recupera sin perder datos (o se pierde
solo lo que la política elegida diga explícitamente); un servidor Linux se da de alta por SSH
sin editar ningún fichero.

---

### Fase 2 — Que el agente no moleste al equipo que vigila

> **`A-05`, `A-06`, `A-07`, `A-09`, `A-03`, `A-08`**
> Esfuerzo estimado: 5 a 7 días · Impacto: **Alto**

**Por qué van juntas.** Todas responden a la misma pregunta —"¿cuánto cuesta tener Hygeia
instalado?"— y todas se miden con el mismo experimento: dejar el agente corriendo una hora en un
equipo y medir CPU, entrada/salida de disco y crecimiento del log. Si se hacen sueltas, ese
experimento se repite seis veces. `A-03` entra aquí, aunque también es corrección, porque su
arreglo mínimo (limitar el drenado por ciclo) es una línea dentro del mismo `agent.go` que se
está tocando, y porque `A-07` (el `PopBatch`) es justo lo que lo hace eficiente. `A-08` es un
cambio de diez minutos dentro del fichero que `A-05` ya está reescribiendo.

**Qué se hace.**
1. Recolector de procesos: sacar la memoria total del bucle, no pedir el estado en Windows, y
   sustituir las dos ordenaciones por un recorrido único. `A-05`, `A-08`
2. Buffer: recuento en memoria, `PopBatch(n)`, y `Push` como operación de añadir con rotación
   por lotes. `A-07`
3. Drenado acotado por ciclo, apoyado en `PopBatch`. `A-03` (arreglo 1)
4. Recolector de CPU por diferencias entre ciclos, con la muestra base tomada durante el jitter
   de arranque. `A-06`
5. Bajar el heartbeat correcto a nivel `Debug`, registrar solo los cambios de estado, y acotar el
   fichero de log con rotación simple. `A-09`

**Cómo se sabe que está hecho.** Una medición antes y después, publicada en el documento de
diseño: uso medio de CPU del proceso, lecturas de disco por minuto con el buffer lleno, y
megabytes de log por día. Son tres números que además sirven de argumento comercial.

---

### Fase 3 — Higiene del repositorio

> **`A-10`, `O-01`, `O-02`, `O-03`, `A-13`, `O-04`**
> Esfuerzo estimado: 3 a 4 días · Impacto: Bajo para el usuario, Alto para el equipo

**Por qué van juntas.** Son todas cambios que tocan muchos ficheros y ninguna lógica. Hacerlas
en bloque significa **un** commit ruidoso en el historial en lugar de seis repartidos, y
significa que el movimiento a `internal/` no entra en conflicto con ninguna corrección en
curso. Por eso van después de las fases 1 y 2, no antes: mover ficheros mientras se corrigen
errores garantiza conflictos de fusión.

**Qué se hace.**
1. Quitar las dos dependencias con versión inválida del `go.mod`. `A-10`
2. Ampliar la integración continua: `gofmt`, `golangci-lint`, `go mod tidy -diff`,
   `govulncheck`. Ejecutar `gofmt -w ./...` sobre todo el repositorio en el mismo commit para
   dejar la base limpia. `O-03`, `A-10`
3. Mover los ocho paquetes del núcleo bajo `internal/`. `O-01`
4. Partir el README en `README.md`, `docs/DISENO.md` y `docs/CONTRATO-INGESTA.md`; escribir el
   `docs/GUIA-GO.md` que el README promete o quitar los enlaces; declarar el contrato como
   fuente única y hacer que el plan del backend enlace a él. `O-02`, `A-13`
5. Detalles: directiva `go:generate` para el arte, `LICENSE`, `CHANGELOG.md`, unificar la fuente
   de la versión, y limpiar el `return` muerto del servidor. `O-04`

**Cómo se sabe que está hecho.** `go mod tidy` se ejecuta sin errores; la integración continua
falla si alguien envía código sin formatear; y alguien que no conoce el proyecto entiende qué es
y cómo se compila leyendo solo el README.

---

### Fase 4 — Operar el agente en campo

> **`F-03`, `F-04`, `F-09`**
> Esfuerzo estimado: 5 a 6 días · Impacto: **Alto**

**Por qué van juntas.** Las tres responden a "el agente está instalado y no reporta, ¿qué hago?".
`F-04` (el subcomando `doctor`) es literalmente el diagnóstico de los fallos que `F-03` (clave
revocada) y `F-09` (proxy o certificado corporativo) provocan; construir el diagnóstico a la vez
que los dos fallos que más lo necesitan hace que el trabajo se refuerce. Además, `F-04` reutiliza
directamente los subcomandos que `F-02` añadió en la fase 1.

**Qué se hace.**
1. Estado `key_rejected`, dejar de llenar el buffer con envíos que nunca se aceptarán, y permitir
   sustituir la clave sin reset previo cuando el servidor ya la ha rechazado. `F-03`
2. Configuración de proxy y de autoridad de certificación adicional, sin debilitar la validación
   del almacén del sistema. `F-09`
3. Subcomando `doctor` con las comprobaciones en cadena: configuración, permisos, formato de
   clave, DNS, conectividad, certificado, autenticación, desviación de reloj, buffer. `F-04`
4. Adaptar el icono de bandeja al estado nuevo, con un texto que diga qué hacer.

**Cómo se sabe que está hecho.** Rotar una clave desde la interfaz de Ellysia y volver a dar de
alta el agente sin editar ficheros ni reiniciar el servicio; y que `doctor` diga exactamente qué
falla en una red con inspección TLS.

---

### Fase 5 — Inventario completo y correcto

> **`F-01`, `A-12`, `F-05`**
> Esfuerzo estimado: 6 a 8 días · Impacto: **Alto**

**Por qué van juntas.** Las tres son el mismo subsistema. `F-01` (Linux y macOS) y `A-12`
(la rama de usuario en Windows) son el mismo trabajo de "hacer que el inventario diga la verdad"
en los tres sistemas, comparten la misma estructura de pruebas y el mismo criterio de
deduplicación. `F-05` (envío diferencial) va detrás por necesidad: no tiene sentido optimizar el
envío de un inventario que todavía está incompleto.

**Qué se hace.**
1. Inventario de Linux: `/var/lib/dpkg/status`, base de datos RPM, y opcionalmente Flatpak y
   Snap. `F-01`
2. Inventario de macOS: `Info.plist` de los paquetes `.app`, más Homebrew si está. `F-01`
3. Inventario de Windows: enumerar `HKEY_USERS` en lugar de `HKEY_CURRENT_USER`, con
   deduplicación entre ramas. Valorar añadir las aplicaciones de la Microsoft Store. `A-12`
4. Hash del inventario y omisión del envío cuando no ha cambiado, con envío forzado diario para
   reconciliar. `F-05`

**Cómo se sabe que está hecho.** El informe en PDF de un servidor Debian y el de un portátil
macOS listan software real; el software instalado por un usuario sin privilegios en Windows
aparece; y el tráfico de inventario baja drásticamente sin que se pierda ningún cambio.

---

### Fase 6 — Distribución

> **`F-08`**
> Esfuerzo estimado: 3 a 4 días de trabajo técnico, más el tiempo de obtener los certificados
> Impacto: **Alto**

**Por qué va sola.** No comparte código con nada: es infraestructura de publicación. Pero va
justo después de la fase 5 por una razón concreta: **solo tiene sentido publicar binarios para
Linux y macOS cuando el inventario funcione en esos sistemas.** Publicar antes sería distribuir
un agente que en Linux reporta un inventario vacío.

Además, es lo que desbloquea la fase `D0` del plan del backend (descargar el agente desde
Ellysia), que se puede construir en el servidor en paralelo una vez existan los artefactos.

**Qué se hace.**
1. Flujo `release.yml` con la matriz de cinco plataformas, disparado por etiqueta.
2. Sumas de verificación y firma de código para Windows y macOS.
3. Integrar el instalador de Windows existente en el flujo.
4. Paquetes `.deb` y `.rpm` con la unidad de systemd.
5. Coordinar con el equipo del servidor el endpoint `GET /hygeia/agent/download`.

---

### Fase 7 — Valor diferencial: seguridad e identidad

> **`F-06`, `F-12`, `F-13`**
> Esfuerzo estimado: 8 a 12 días · Impacto: **Alto**

**Por qué van juntas.** Las tres amplían lo que el agente *observa* y las tres requieren
ampliación coordinada del contrato y del servidor. `F-06` (puertos a la escucha) y `F-12`
(direcciones de red) usan la misma parte de `gopsutil` y se implementan del tirón. `F-13`
(identidad del equipo) comparte con `F-12` el mismo bloque nuevo del payload y la misma
migración en el servidor.

Esta es la fase que convierte a Hygeia de "monitorización de salud" en "señal de seguridad", que
es lo que el §1 del plan del backend describe como el encaje del módulo en la misión de Ellysia.

**Qué se hace.**
1. Detección de puertos nuevos a la escucha y de procesos con CPU alta en rutas sospechosas,
   emitidos por el campo `localAlerts` que el contrato ya tiene. `F-06`
2. Trabajo en el servidor: persistir `localAlerts` y convertirlas en anomalías con su ciclo de
   vida, reutilizando el modelo `Anomaly` que ya existe. `F-06`
3. Direcciones IP y MAC en el bloque `host`. `F-12`
4. Bloque de identidad del equipo (fabricante, modelo, dominio, virtualización) y etiquetado
   automático en el servidor. `F-13`
5. Los inicios de sesión fallidos quedan fuera de esta fase; se valoran después con los datos de
   uso reales de las dos primeras señales.

---

### Fase 8 — Ampliación del contrato de métricas

> **`F-07`, `A-11`**
> Esfuerzo estimado: 4 a 6 días · Impacto: Medio

**Por qué van juntas.** Son la misma operación: subir el contrato a la versión `1.1`. Hacerlo de
una vez significa una migración de base de datos, una revisión del esquema y una entrega de la
interfaz web, en lugar de cinco de cada. `A-11` (los dos campos que hoy mienten) entra aquí
porque corrige el mismo contrato que se está ampliando, y porque cambiar el significado de
`errIn`/`errOut` en datos ya persistidos merece anunciarse junto a un cambio de versión.

**Qué se hace.**
1. Decidir qué métricas nuevas se van a mostrar en la interfaz antes de escribir código.
2. Entrada/salida de disco e IOPS por dispositivo, como tasa. `F-07`
3. Usuarios conectados, y las demás métricas del §3 que se decidan. `F-07`
4. Corregir `errIn`/`errOut` para que sean tasa, y resolver `ctxSwitches` (medirlo o marcarlo
   como omisible). `A-11`
5. Ampliar el esquema del servidor, la migración y la interfaz.

---

### Fase 9 — Gestión de flota

> **`F-10`**
> Esfuerzo estimado: 6 a 8 días · Impacto: Medio

**Por qué va al final.** No resuelve ningún problema que exista hoy: con pocos equipos, editar
la configuración a mano es viable. Solo empieza a doler cuando la flota crece. Y para entonces
todo lo anterior debería estar hecho, porque configurar remotamente un agente que pierde datos
o que no sabe reportar inventario no arregla nada.

---

### Aplazada — Actualización automática

> **`F-11`**

Descartada conscientemente, no por olvido. Se reevalúa cuando la fase 6 esté cerrada y la flota
sea lo bastante grande como para que visitar cada equipo sea inviable. Hasta entonces, las
herramientas de despliegue que el cliente ya tiene son una solución más segura y con coste de
construcción cero.

---

### Resumen visual de dependencias entre fases

```
Fase 1  Que los datos lleguen
   │      (sin esto, cualquier otra mejora es sobre datos que se pierden)
   ├──> Fase 2  Coste sobre el equipo
   │
   ├──> Fase 3  Higiene del repositorio
   │       (después de 1 y 2 para no provocar conflictos de fusión)
   │
   ├──> Fase 4  Operación en campo
   │       (usa los subcomandos que añade la fase 1)
   │
   └──> Fase 5  Inventario completo
            │
            └──> Fase 6  Distribución
                    (publicar binarios de Linux exige que Linux funcione)
                    │
                    └──> Fase D0 del servidor: descarga desde Ellysia
                    └──> Fase 11 (aplazada): actualización automática

Fase 7  Seguridad e identidad     ─┐
Fase 8  Ampliación del contrato   ─┼─ independientes entre sí;
Fase 9  Gestión de flota          ─┘  requieren trabajo en el servidor
```

---

## 8. Anexo A — Cómo comprobar cada hallazgo

Los hallazgos de este documento son verificables. Estos son los comandos y las referencias
exactas para reproducir los principales.

**`A-01` — El porcentaje de CPU por proceso puede superar 100.**
Comparar la fórmula de [`collector/processes.go:91`](../collector/processes.go) con la
validación de `EllysiaServer/API/src/modules/features/hygeia/schemas.py:180`. Para observarlo en
vivo, ejecutar el agente en primer plano en una máquina de varios núcleos mientras se satura la
CPU con dos o más procesos, y mirar el JSON que se envía.

**`A-02` — Buffer contra ventana de reloj.**
La capacidad está en [`agent/agent.go:55`](../agent/agent.go); la ventana, en
`EllysiaServer/API/SecOpsConfig.json`, campo `features.hygeia.limits.clockSkewSec`.

**`A-03` — Intervalo mínimo y drenado.**
`minIntervalSec` en el mismo fichero de configuración del servidor; la comprobación en
`managers.py:836`; el bucle de drenado en [`agent/agent.go:389`](../agent/agent.go).

**`A-05` — Llamadas redundantes por proceso.**
```bash
cat "$(go env GOMODCACHE)/github.com/shirou/gopsutil/v4@v4.26.6/process/process.go" | sed -n '342,356p'
```
Se ve que `MemoryPercentWithContext` llama a `mem.VirtualMemoryWithContext` en cada invocación.
Para el caso de Windows:
```bash
sed -n '464,466p' "$(go env GOMODCACHE)/github.com/shirou/gopsutil/v4@v4.26.6/process/process_windows.go"
```

**`A-10` — `go.mod` inválido.**
```bash
go mod verify
go list -m all
```
Ambos fallan con `invalid version: unknown revision v0.0.0-latest`, mientras que
`go build ./...` funciona.

**`O-03` — Formato no verificado.**
En un clon con finales de línea LF (o dentro de la integración continua):
```bash
gofmt -l .
```
Aparece, entre otros, `payload/payload.go`, por la alineación rota de la estructura `Software`.

> Nota para quien lo ejecute en Windows: el fichero `.gitattributes` declara `* text=auto`, así
> que en el árbol de trabajo local los ficheros tienen finales de línea CRLF y `gofmt -l` los
> marca **todos**. Eso es un artefacto local, no un problema real: en el repositorio y en la
> integración continua los finales son LF. Para comprobarlo de verdad hay que normalizar antes:
> ```bash
> tr -d '\r' < payload/payload.go > /tmp/payload.go && gofmt -d /tmp/payload.go
> ```

**`A-13` / `O-02` — Contrato duplicado y desactualizado.**
El bloque JSON del §9 del [`README.md`](../README.md) no contiene el campo `inventory`, que sí
está en [`payload/payload.go:21`](../payload/payload.go) y en `schemas.py:279`.

**Estado de las pruebas actuales.**
```bash
go test ./...
```
Todas pasan (verificado el 12 de agosto de 2026). Los paquetes `cmd/hygeia-agent`,
`cmd/hygeia-tray`, `internal/autostart`, `internal/icon/gen` y `version` no tienen pruebas; los
tres primeros son los que más las echarían de menos.

---

*Documento generado el 12 de agosto de 2026 a partir del análisis del código de
`HygeiaAgent` (commit `65b9206`, rama `develope`) y del módulo `hygeia` de `EllysiaServer`.
Los identificadores `A-nn`, `O-nn` y `F-nn` son estables: se pueden usar en issues y en
conversaciones sin ambigüedad.*
