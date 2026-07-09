# Guía de Go — instalación, programación y compilación

> Guía pensada para alguien que **no ha programado nunca en Go**. Cubre la
> instalación, la sintaxis completa del lenguaje, la stdlib esencial, concurrencia,
> testing, módulos y compilación. Es autocontenida: no depende de ningún proyecto
> concreto. Si ya sabes programar en otro lenguaje, léela en orden. Si vienes de
> cero absoluto, ve directo al §3 (hola mundo) y vuelve al §2 cuando quieras
> entender los fundamentos.

---

## Índice

1. [Qué es Go](#1-qué-es-go)
2. [Instalación](#2-instalación)
3. [Hola mundo y primeras herramientas](#3-hola-mundo-y-primeras-herramientas)
4. [Fundamentos del lenguaje](#4-fundamentos-del-lenguaje)
5. [Estructuras de datos](#5-estructuras-de-datos)
6. [Métodos e interfaces](#6-métodos-e-interfaces)
7. [Manejo de errores](#7-manejo-de-errores)
8. [Concurrencia](#8-concurrencia)
9. [Tour por la biblioteca estándar](#9-tour-por-la-biblioteca-estándar)
10. [Módulos y dependencias](#10-módulos-y-dependencias)
11. [Testing](#11-testing)
12. [Compilación y cross-compilación](#12-compilación-y-cross-compilación)
13. [Herramientas y buenas prácticas](#13-herramientas-y-buenas-prácticas)
14. [Organización de proyectos Go](#14-organización-de-proyectos-go)
15. [Recursos oficiales](#15-recursos-oficiales)

---

## 1. Qué es Go

Go (también llamado *Golang*) es un lenguaje **compilado**, con **tipado
estático**, creado por Google en 2009. Fue diseñado por Robert Griesemer, Rob
Pike y Ken Thompson con tres objetivos: simplicidad, velocidad de compilación
y concurrencia tratable.

### Rasgos distintivos

- **Un solo binario estático.** Compilas y obtienes un ejecutable que no necesita
  runtime ni bibliotecas externas instaladas en el sistema destino. Lo copias y
  funciona.
- **Compilación rapidísima.** Proyectos grandes compilan en segundos, no en
  minutos. El compilador hace linking estático por defecto.
- **Cross-compilación nativa.** Desde cualquier SO produces binarios para
  cualquier otro SO/arquitectura cambiando dos variables de entorno, sin
  toolchains adicionales.
- **Goroutines.** Concurrencia ligera: lanzas miles de tareas concurrentes que
  ocupan ~2 KB de stack cada una (frente a ~1 MB de un hilo del SO). El runtime
  multiplexa las goroutines sobre hilos del sistema automáticamente.
- **Canales.** Comunicación tipada entre goroutines inspirada en CSP
  (*Communicating Sequential Processes*). El mantra: *"no compartas memoria para
  comunicarte; comunícate compartiendo memoria"*.
- **Recolector de basura.** Liberación automática de memoria, con baja latencia.
- **No hay clases ni herencia.** Usa `struct` + `interface` + composición.
- **No hay excepciones.** Los errores son **valores** que devuelves y compruebas.
  El flujo de error es explícito, no mágico.
- **Lenguaje pequeño.** La especificación completa cabe en unas 50 páginas. La
  sintaxis se aprende en un fin de semana.

### Cuándo usar Go

Aplicaciones de red, servidores HTTP, CLI, agentes, microservicios,
infraestructura (Docker, Kubernetes, Terraform, Prometheus están escritos en Go),
herramientas de desarrollo. **No** es la mejor opción para aplicaciones de
escritorio con GUI, videojuegos 3D o sistemas embebidos muy limitados (aunque
TinyGo cubre parte de esto).

---

## 2. Instalación

### 2.1 Windows

**Opción A — instalador oficial (recomendada):**
1. Ve a https://go.dev/dl/
2. Descarga `go1.22.x.windows-amd64.msi` (o la versión más reciente).
3. Ejecútalo. Por defecto instala en `C:\Program Files\Go` y **añade Go al PATH
   automáticamente**.
4. Abre una terminal **nueva** (PowerShell o Símbolo del sistema) y verifica:

```powershell
go version
# go version go1.22.x windows/amd64
```

**Opción B — winget:**
```powershell
winget install GoLang.Go
```

**Opción C — scoop:**
```powershell
scoop install go
```

> Si `go` no se reconoce tras instalar, abre una terminal nueva. Si sigue sin
> reconocerse, añade `C:\Program Files\Go\bin` a la variable de entorno `PATH`.

### 2.2 Linux

```bash
# Descarga (sustituye amd64 por arm64 si tu máquina es ARM)
wget https://go.dev/dl/go1.22.x.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.x.linux-amd64.tar.gz

# Añade al PATH (~/.bashrc o ~/.zshrc)
export PATH=$PATH:/usr/local/go/bin

# Aplica los cambios
source ~/.bashrc
go version
```

En Debian/Ubuntu también funciona `sudo apt install golang-go`, pero la versión
de los repos suele estar varias releases por detrás. El tarball oficial es
preferible.

### 2.3 macOS

```bash
brew install go
go version
```

### 2.4 Variables de entorno

| Variable | Significado | Valor típico |
|---|---|---|
| `GOROOT` | Dónde está instalado Go | `C:\Program Files\Go` o `/usr/local/go` |
| `GOPATH` | Espacio de trabajo; binarios instalados con `go install` | `~/go` (por defecto) |
| `GOMODCACHE` | Caché de módulos descargados | `~/go/pkg/mod` |
| `GOOS` / `GOARCH` | SO y arquitectura destino (para cross-compile) | `linux`/`amd64` |
| `GOPROXY` | Proxy de módulos | `https://proxy.golang.org,direct` |
| `GOPRIVATE` | Módulos privados que no consultan el proxy | repos de tu organización |
| `CGO_ENABLED` | Habilitar compilador de C (0 = binario puramente Go) | `1` (cambiar a `0` para estático) |

Consulta todas con:
```powershell
go env
```

> **No necesitas** crear `$GOPATH/src/...` para tus proyectos. Desde Go 1.11
> (2018) se trabaja con **módulos** (§10) y puedes clonar tu proyecto en
> cualquier carpeta del disco.

### 2.5 Editor

- **VS Code** + extensión oficial **"Go"** (de Google, `golang.go`). Instala
  `gopls` (el servidor de lenguaje) automáticamente. Es lo recomendado para
  empezar y es gratis.
- **GoLand** (JetBrains, de pago): IDE completo para Go.
- `gopls` proporciona autocompletado, ir-a-definición, diagnósticos en vivo,
  formateo al guardar y sugerencias de refactorización.
- **Herramientas adicionales** que VS Code te ofrecerá instalar: `dlv`
  (depurador), `staticcheck` (linter avanzado), `gotests` (generación de tests).
  Acepta todas.

---

## 3. Hola mundo y primeras herramientas

### 3.1 Tu primer programa

Crea una carpeta y un archivo `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hola, Go")
}
```

Inicializa un módulo y ejecuta:

```powershell
go mod init hola
go run .
```

- `package main` es el nombre del paquete raíz. Solo los paquetes `main`
  producen un ejecutable.
- `func main()` es el punto de entrada (sin argumentos ni retorno, a diferencia
  de C).
- `import "fmt"` trae el paquete `fmt` de la stdlib para imprimir. Go **exige**
  que todo import se use y que no haya variables sin usar: si importas algo y no
  lo usas, no compila.
- `go run .` compila en memoria y ejecuta (sin dejar binario).
- `go build` compila y deja un binario en la carpeta actual.

### 3.2 Los tres comandos que más vas a usar

| Comando | Efecto |
|---|---|
| `go run .` | Compila y ejecuta (sin binario persistente). |
| `go build .` | Compila y deja el binario en el directorio actual. |
| `go build -o nombre.exe .` | Ídem, pero con nombre de salida explícito. |
| `go fmt ./...` | Formatea todo el código del módulo. |
| `go vet ./...` | Analiza el código en busca de errores comunes. |
| `go test ./...` | Ejecuta todos los tests del módulo. |
| `go mod tidy` | Añade/quita dependencias de `go.mod` y actualiza `go.sum`. |
| `go get pkg@v1.2.3` | Añade/actualiza una dependencia. |

El punto `.` significa "el paquete del directorio actual". `./...` significa
"este paquete y todos sus subpaquetes".

---

## 4. Fundamentos del lenguaje

### 4.1 Paquetes e imports

En Go, **cada directorio es un paquete**. El nombre del paquete va en la primera
línea de cada archivo `.go` dentro del directorio:

```go
package calculadora
```

Todos los archivos de un mismo directorio deben pertenecer al mismo paquete
(con la excepción de `_test.go`, que pueden usar `package calculadora_test`).

Para usar algo de otro paquete lo importas por su ruta de módulo:

```go
import (
	"fmt"
	"math"
	"mimodulo.com/auth"
)
```

Lo que **empieza con mayúscula** se exporta (es público). Lo que empieza con
**minúscula** es privado al paquete. No existen las palabras `public`,
`private`, `protected`:

```go
func Suma(a, b int) int { return a + b }   // exportada
func resta(a, b int) int { return a - b }  // privada
```

### 4.2 Variables, constantes y tipos básicos

```go
// Declaración con var + tipo explícito
var nombre string = "Go"
var edad int          // cero-value = 0
var activo bool       // cero-value = false
var pi float64 = 3.14

// Declaración corta con inferencia de tipo (solo dentro de funciones)
saludo := "Hola"           // string
contador := 0                // int
precio := 9.99               // float64
hecho := false               // bool

// Bloque var
var (
	host = "localhost"
	port = 8080
)

// Constantes (el tipo se infiere del contexto)
const MaxRetries = 3
const Version = "1.0.0"
```

**Tipos numéricos:**

| Tipo | Rango |
|---|---|
| `int`, `uint` | 32 o 64 bits según arquitectura |
| `int8`, `int16`, `int32`, `int64` | Enteros con signo |
| `uint8` (`byte`), `uint16`, `uint32`, `uint64` | Enteros sin signo |
| `float32`, `float64` | Coma flotante |
| `complex64`, `complex128` | Números complejos |

**En la práctica** casi siempre usarás `int` para enteros y `float64` para
decimales. `uint64` es común para tamaños (bytes), offsets o contadores que
nunca son negativos.

**Cero-values:** toda variable declarada sin valor explícito recibe un **valor
cero** del tipo: `0` para numéricos, `""` para strings, `false` para bool,
`nil` para punteros, slices, maps, interfaces y funciones.

### 4.3 Funciones

```go
// Básica
func suma(a, b int) int {
	return a + b
}

// Múltiples parámetros del mismo tipo (notación compacta)
func multiplicar(a, b, c int) int {
	return a * b * c
}

// Retorno múltiple
func dividir(a, b int) (int, error) {
	if b == 0 {
		return 0, fmt.Errorf("no se puede dividir por cero")
	}
	return a / b, nil
}

// Retornos con nombre (los inicializa al cero-value)
func split(sum int) (x, y int) {
	x = sum * 4 / 9
	y = sum - x
	return // "naked return": devuelve x e y
}

// Función variádica (número variable de argumentos)
func sumar(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}
// Uso: sumar(1, 2, 3, 4)

// Las funciones son valores de primera clase
operacion := suma
resultado := operacion(3, 4)

// Closure
func contador() func() int {
	i := 0
	return func() int {
		i++
		return i
	}
}
```

### 4.4 Control de flujo

**`if`:** no usa paréntesis, pero las llaves son obligatorias. Puede incluir una
sentencia corta antes de la condición:

```go
if err != nil {
	return err
}

// Declaración + condición en una línea (muy común)
if x := calcular(); x < 0 {
	fmt.Println("negativo")
} else if x == 0 {
	fmt.Println("cero")
} else {
	fmt.Println("positivo")
}
// x solo existe dentro del bloque if/else
```

**`for`:** es el **único** bucle del lenguaje. Sirve como `for`, `while` e
infinito:

```go
// Estilo clásico
for i := 0; i < 10; i++ {
	fmt.Println(i)
}

// Estilo while
suma := 0
for suma < 100 {
	suma += rand.Intn(20)
}

// Bucle infinito
for {
	// se sale con break, return, etc.
}

// Iterar con range (sobre slices, arrays, maps, strings, canales)
nums := []int{2, 4, 6, 8}
for i, v := range nums {
	fmt.Printf("índice %d, valor %d\n", i, v)
}
```

**`switch`:** no tiene *fall-through* implícito (no necesitas `break`). Puede
evaluar condiciones, no solo valores:

```go
switch dia {
case "lunes":
	fmt.Println("inicio de semana")
case "viernes":
	fmt.Println("casi finde")
default:
	fmt.Println("día normal")
}

// Switch sin expresión = if/else encadenado más limpio
switch {
case hora < 12:
	fmt.Println("buenos días")
case hora < 18:
	fmt.Println("buenas tardes")
default:
	fmt.Println("buenas noches")
}
```

### 4.5 Punteros

Go tiene punteros pero **no** aritmética de punteros (no puedes sumar/restar
a una dirección). Son seguros por diseño.

```go
x := 42
p := &x      // p es *int (puntero a int), contiene la dirección de x
fmt.Println(*p) // 42: desreferenciar (leer el valor)
*p = 21         // escribir a través del puntero
fmt.Println(x)  // 21: x ha cambiado

// Puntero a struct
type Persona struct {
	Nombre string
}

func renombrar(p *Persona, nombre string) {
	p.Nombre = nombre // sintaxis abreviada: no necesitas (*p).Nombre
}

// new(T) devuelve *T apuntando a un cero-value
ptr := new(int)    // *int, valor 0

// nil = puntero que no apunta a nada
var q *int
fmt.Println(q == nil) // true
```

**Cuándo usar punteros:**
- Para que una función **modifique** el valor del llamante.
- Para evitar copias de structs grandes (aunque el compilador a veces optimiza).
- Para distinguir entre "ausencia" y "valor cero" (un puntero puede ser `nil`; un
  `int` siempre es al menos `0`).

### 4.6 `defer`

`defer` pospone la ejecución de una llamada hasta que la función retorna. Se usa
para **limpiar recursos** (cerrar archivos, liberar locks, etc.):

```go
func leerArchivo(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // se ejecuta al salir de la función, pase lo que pase

	return io.ReadAll(f)
}
```

Los `defer` se ejecutan en orden **LIFO** (el último declarado es el primero en
ejecutarse). Valores de parámetros se evalúan en el momento de la declaración,
no de la ejecución.

### 4.7 `panic` y `recover`

`panic` detiene la ejecución normal y desenrolla la pila (ejecutando `defer`s)
hasta que alguien hace `recover` o el programa termina. **No** es el mecanismo
de error habitual de Go: se reserva para errores irrecuperables (bug del
programador).

```go
func debeSerPositivo(n int) {
	if n < 0 {
		panic("n no puede ser negativo")
	}
}

func seguro(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recuperado: %v", r)
		}
	}()
	f()
	return
}
```

Regla general: **no paniquees en bibliotecas**; devuelve `error`. Reserva
`panic` para `main` o funciones `init` ante fallos de configuración que impiden
arrancar.

### 4.8 Genéricos (Go 1.18+)

Desde Go 1.18, el lenguaje tiene **genéricos** (parámetros de tipo):

```go
// Función genérica: funciona con cualquier tipo ordenable
func Min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

x := Min(3, 5)        // T = int
y := Min(1.5, 2.3)    // T = float64

// Struct genérico
type Pila[T any] struct {
	items []T
}

func (p *Pila[T]) Push(item T) {
	p.items = append(p.items, item)
}

func (p *Pila[T]) Pop() (T, bool) {
	if len(p.items) == 0 {
		var zero T
		return zero, false
	}
	item := p.items[len(p.items)-1]
	p.items = p.items[:len(p.items)-1]
	return item, true
}
```

`constraints.Ordered` y `constraints` se movieron a `golang.org/x/exp/constraints`
en Go 1.22+. La restricción `any` es un alias de `interface{}`. Puedes definir
tus propias restricciones con interfaces que contengan tipos:

```go
type Numero interface {
	int | int64 | float64
}

func Doble[T Numero](v T) T { return v * 2 }
```

Los genéricos en Go son menos ubicuos que en Java o Rust. Se usan principalmente
en **bibliotecas de estructuras de datos** y **algoritmos reutilizables**. En
aplicación, la mayoría del código sigue sin usarlos.

---

## 5. Estructuras de datos

### 5.1 Arrays

Longitud fija, parte del tipo:

```go
var a [3]int                    // [0, 0, 0]
b := [3]int{1, 2, 3}            // [1, 2, 3]
c := [...]int{1, 2, 3, 4}       // infiere tamaño: [4]int
d := [3]int{0: 10, 2: 30}       // índices explícitos: [10, 0, 30]

// [3]int y [4]int son TIPOS DISTINTOS
```

Los arrays se usan poco directamente; la estructura ubicua es el **slice**.

### 5.2 Slices

Un slice es una **vista flexible** sobre un array subyacente. Tiene longitud
(`len`) y capacidad (`cap`):

```go
// Crear slices
var s []int                       // nil, len=0
s = []int{1, 2, 3}                // literal
s = make([]int, 5)                // 5 ceros, len=5, cap=5
s = make([]int, 5, 10)            // len=5, cap=10

// Acceder y modificar
s[0] = 99

// Rebanar
sub := s[1:3]                     // elementos 1 y 2 (semiabierto: [1,3))

// Añadir elementos
s = append(s, 4)                   // crece dinámicamente
s = append(s, 5, 6, 7)            // varios a la vez
s = append(s, otros...)

// Copiar
copia := make([]int, len(s))
copy(copia, s)

// Longitud y capacidad
fmt.Println(len(s), cap(s))
```

**Truco mental:** `s[low:high]` incluye `low` y excluye `high`.
`s[:]` es el slice entero. `s[low:]` desde `low` hasta el final.
`s[:high]` desde el principio hasta `high` (excluido).

**Peligro común:** rebanar no copia, apunta al mismo array subyacente. Si
modificas un elemento en `sub`, puede cambiar `s` también. Para independizarlo:
`sub := make([]T, n); copy(sub, original)`.

### 5.3 Maps

Diccionario / tabla hash desordenada:

```go
// Crear
edades := map[string]int{
	"Ana":  28,
	"Luis": 35,
}
vacio := make(map[string]int)       // map vacío
var nulo map[string]int              // nil (no se puede escribir)

// Leer y escribir
edades["Carlos"] = 42                // insertar/actualizar
edad := edades["Ana"]                // leer (si no existe, cero-value: 0)

// Comprobar existencia
edad, ok := edades["John"]           // ok=false si no existe
if !ok {
	fmt.Println("no encontrado")
}

// Eliminar
delete(edades, "Luis")

// Iterar (orden NO garantizado)
for nombre, edad := range edades {
	fmt.Printf("%s tiene %d años\n", nombre, edad)
}
```

### 5.4 Structs

Agrupación de campos con nombre y tipo:

```go
type Persona struct {
	Nombre   string
	Edad     int
	Activo   bool
}

// Crear
p1 := Persona{"Ana", 28, true}            // por posición (frágil, evitar)
p2 := Persona{Nombre: "Ana", Edad: 28}     // por nombre (idiomático)
p3 := Persona{}                             // cero-value: "", 0, false

// Acceder
p2.Nombre = "Ana María"
fmt.Println(p2.Edad)

// Campos incrustados (composición, no herencia)
type Empleado struct {
	Persona           // "embedding": promociona campos de Persona
	Salario  float64
}

e := Empleado{Persona: Persona{Nombre: "Luis"}, Salario: 50000}
fmt.Println(e.Nombre)  // campo promocionado
fmt.Println(e.Persona.Nombre) // acceso explícito (equivalente)
```

Los **tags** de struct son cadenas que anotan campos para bibliotecas (`json`,
`xml`, `yaml`, `toml`, validación, ORMs...). Van entre backticks:

```go
type Config struct {
	Host string `json:"host" yaml:"host" validate:"required"`
	Port int    `json:"port" yaml:"port"`
}
```

---

## 6. Métodos e interfaces

### 6.1 Métodos

Un método es una función con un **receptor**:

```go
type Contador struct {
	valor int
}

// Receptor por valor (no muta)
func (c Contador) Valor() int {
	return c.valor
}

// Receptor por puntero (muta el struct)
func (c *Contador) Incrementar() {
	c.valor++
}

// Uso
c := Contador{}
c.Incrementar() // Go convierte c en &c automáticamente
c.Valor()       // 1
```

Puedes definir métodos sobre cualquier tipo definido en tu paquete (excepto
punteros e interfaces).

### 6.2 Interfaces

Una interfaz declara un **conjunto de métodos**. Cualquier tipo que implementa
todos esos métodos **automáticamente** satisface la interfaz. **No necesitas
declarar `implements`**: es satisfacción implícita.

```go
// Declaración
type Hablador interface {
	Hablar() string
}

// Implementación implícita
type Perro struct{ Nombre string }

func (p Perro) Hablar() string {
	return "Guau, soy " + p.Nombre
}

type Gato struct{ Nombre string }

func (g Gato) Hablar() string {
	return "Miau, soy " + g.Nombre
}

// Polimorfismo: cualquier Hablador
func presentar(h Hablador) {
	fmt.Println(h.Hablar())
}

// Uso
presentar(Perro{"Firulais"})
presentar(Gato{"Michi"})
```

**Interfaz vacía `any`** (antes `interface{}`): todos los tipos la satisfacen.
Úsala con moderación; pierdes seguridad de tipos:

```go
var x any = "hola"
s, ok := x.(string)   // type assertion
```

**Patrones comunes de interfaces:**

```go
// io.Reader y io.Writer: las interfaces más importantes de Go
type Reader interface {
	Read(p []byte) (n int, err error)
}
type Writer interface {
	Write(p []byte) (n int, err error)
}

// error: la interfaz más simple
type error interface {
	Error() string
}

// fmt.Stringer: como __str__ en Python
type Stringer interface {
	String() string
}
```

**Consejo:** define interfaces donde las **consumes**, no donde las
**implementas**. Si tu paquete acepta algo que necesita leer, define `Reader`
allí o usa el de `io`. No definas interfaces gigantes: las de la stdlib suelen
tener 1-3 métodos.

---

## 7. Manejo de errores

Go no tiene excepciones, `try`/`catch` ni `throw`. Los errores son **valores**
que se devuelven y se comprueban explícitamente. El tipo `error` es una interfaz
con un solo método:

```go
type error interface {
	Error() string
}
```

### 7.1 El patrón estándar

```go
func dividir(a, b int) (int, error) {
	if b == 0 {
		return 0, fmt.Errorf("división por cero: %d/%d", a, b)
	}
	return a / b, nil
}

resultado, err := dividir(10, 0)
if err != nil {
	fmt.Println("falló:", err)
	return
}
fmt.Println("resultado:", resultado)
```

Un `nil` en la posición de error significa "todo bien". La convención más
importante del lenguaje: **comprueba siempre el error**. El compilador no te
obliga, pero tu código no será idiomático si no lo haces.

### 7.2 Crear errores

```go
// Error simple
err := errors.New("algo salió mal")

// Error con formato
err := fmt.Errorf("archivo %s: %w", nombre, errOriginal)

// Error personalizado
type ErrorValidacion struct {
	Campo   string
	Mensaje string
}
func (e *ErrorValidacion) Error() string {
	return fmt.Sprintf("validación de %s: %s", e.Campo, e.Mensaje)
}
```

### 7.3 Envolver, inspeccionar y desempaquetar (Go 1.13+)

```go
// Envolver: %w en fmt.Errorf conserva el error original
err := fmt.Errorf("conexión a BD falló: %w", sql.ErrNoRows)

// errors.Is: comprueba si un error ES otro (recorre la cadena de %w)
if errors.Is(err, sql.ErrNoRows) {
	// manejar "no encontrado"
}

// errors.As: desempaqueta un error a un tipo concreto
var valErr *ErrorValidacion
if errors.As(err, &valErr) {
	fmt.Println("campo:", valErr.Campo)
}
```

- `==` compara errores por identidad (no por texto).
- `errors.Is` funciona aunque el error esté envuelto con `%w` varias veces.
- `errors.As` extrae el error concreto sin importar cuántas capas tenga.

### 7.4 Buenas prácticas

- No uses `panic` para errores de negocio. `panic` es para bugs del programador.
- Añade contexto al error al subir la pila: `fmt.Errorf("leyendo config: %w", err)`.
- No pongas la palabra "error" en el mensaje (es redundante).
- No compares errores por su mensaje de texto (`err.Error() == "..."`). Usa
  `errors.Is` o variables centinela.
- En el nivel más alto (main), loguea o imprime el error y termina con gracia.

---

## 8. Concurrencia

La concurrencia es el rasgo más distintivo de Go. Dos primitivas: **goroutines**
(ejecución concurrente) y **canales** (comunicación entre goroutines).

### 8.1 Goroutines

Una goroutine es una tarea que se ejecuta concurrentemente. Lanzarla cuesta casi
nada (unos KB de stack):

```go
func imprimir(msg string) {
	for i := 0; i < 3; i++ {
		fmt.Println(msg, i)
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	go imprimir("goroutine")   // se lanza y continúa
	imprimir("main")           // se ejecuta en la goroutine principal
	// "main" y "goroutine" se intercalan
}
```

El programa termina cuando la goroutine principal (`main`) termina, aunque haya
goroutines hijas ejecutándose. Para esperarlas, usa `sync.WaitGroup` o canales.

### 8.2 `sync.WaitGroup`

```go
func main() {
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {       // ⚠️ pasa id como parámetro, no captures la i del bucle
			defer wg.Done()
			fmt.Printf("tarea %d\n", id)
		}(i)
	}

	wg.Wait()  // bloquea hasta que todas las goroutines llamen Done()
	fmt.Println("todas las tareas terminaron")
}
```

**Regla de oro:** pasa las variables del bucle como argumento a la goroutine. Si
capturas `i` directamente, todas las goroutines verán el último valor del bucle
(el compilador avisa de esto desde Go 1.22, pero el argumento explícito sigue
siendo buena práctica).

### 8.3 Canales

Un canal es un conducto tipado por el que las goroutines envían y reciben
valores. Es la forma idiomática de comunicarse:

```go
ch := make(chan int)       // canal sin búfer (bloquea hasta que alguien recibe)
ch := make(chan int, 10)   // canal con búfer de 10 elementos

// Enviar y recibir
ch <- 42                   // enviar
valor := <-ch              // recibir
valor, ok := <-ch          // recibir con comprobación (ok=false si cerrado)

// Cerrar
close(ch)

// Iterar sobre un canal hasta que se cierra
for v := range ch {
	fmt.Println(v)
}
```

**Canal sin búfer:** el emisor se bloquea hasta que un receptor lee.
**Canal con búfer:** el emisor se bloquea solo cuando el búfer se llena.

### 8.4 `select`

`select` espera a que **una** de varias operaciones de canal esté lista. Si
varias están listas, elige una al azar (para evitar inanición):

```go
func main() {
	ch1 := make(chan string)
	ch2 := make(chan string)

	go func() {
		time.Sleep(1 * time.Second)
		ch1 <- "uno"
	}()
	go func() {
		time.Sleep(2 * time.Second)
		ch2 <- "dos"
	}()

	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch1:
			fmt.Println("ch1:", msg)
		case msg := <-ch2:
			fmt.Println("ch2:", msg)
		case <-time.After(3 * time.Second):
			fmt.Println("timeout")
		}
	}
}
```

**Patrones con `select`:**

```go
// Timeout
select {
case res := <-ch:
	fmt.Println(res)
case <-time.After(5 * time.Second):
	fmt.Println("demasiado lento")
}

// Bucle infinito con salida limpia
for {
	select {
	case <-ctx.Done():
		return
	case msg := <-ch:
		procesar(msg)
	}
}
```

### 8.5 `context.Context`

`context.Context` transporta **plazos, cancelación y valores** a través de las
llamadas. Es ubicuo en servidores HTTP,workers y cualquier código concurrente:

```go
// Crear contextos
ctx := context.Background()                        // raíz (nunca cancelado)
ctx, cancel := context.WithCancel(ctx)              // cancelable manualmente
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)  // con plazo
ctx, cancel := context.WithDeadline(ctx, fecha)         // con fecha límite

// Cancelar (llamar siempre con defer)
defer cancel()

// Valores (usar con moderación, solo para datos de request-scope)
ctx = context.WithValue(ctx, "clave", valor)
v := ctx.Value("clave")

// Respetar cancelación en operaciones bloqueantes
func trabajar(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()  // context.Canceled o context.DeadlineExceeded
	case resultado := <-tareaLenta():
		return procesar(resultado)
	}
}
```

**Reglas de `context`:**
- Pásalo siempre como **primer parámetro** de la función.
- No lo guardes en structs; pásalo explícitamente.
- `context.Background()` solo en `main`, tests y código de entrada.
- `context.TODO()` cuando no sabes qué contexto usar (marcador para implementar
  después).
- Los valores de contexto son para datos de alcance limitado (ID de petición,
  trazabilidad). NUNCA para parámetros de negocio opcionales.

### 8.6 `sync.Mutex` y `sync.RWMutex`

Para cuando realmente necesitas compartir estado mutable entre goroutines:

```go
type ContadorSeguro struct {
	mu    sync.Mutex
	valor int
}

func (c *ContadorSeguro) Incrementar() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valor++
}

func (c *ContadorSeguro) Valor() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.valor
}

// RWMutex permite múltiples lectores simultáneos
type Cache struct {
	mu    sync.RWMutex
	datos map[string]string
}

func (c *Cache) Get(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.datos[key]
}

func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.datos[key] = value
}
```

Otras primitivas: `sync.Once` (ejecutar exactamente una vez), `sync.Map`
(mapa concurrente, rara vez mejor que `map` + `Mutex`), `sync.Pool` (pool de
objetos temporales), `sync.Cond` (condición de espera).

---

## 9. Tour por la biblioteca estándar

Go tiene una stdlib extensa y de alta calidad. Aquí los paquetes que más vas a
usar.

### 9.1 `fmt` — impresión con formato

```go
fmt.Print("sin salto de línea")
fmt.Println("con salto de línea")
fmt.Printf("formato: %s tiene %d años\n", "Ana", 28)

// Verbos comunes
// %s   string
// %d   entero decimal
// %f   float
// %v   valor por defecto (el más usado)
// %+v  valor con nombres de campo (structs)
// %#v  representación Go-sintaxis
// %T   tipo del valor
// %t   bool
// %q   string con comillas

mensaje := fmt.Sprintf("resultado: %v", 42)  // devuelve string, no imprime
```

### 9.2 `encoding/json`

```go
type Persona struct {
	Nombre string `json:"nombre"`
	Edad   int    `json:"edad,omitempty"`
	private string // campo no exportado (minúscula) -> ignorado por JSON
}

// Marshal: struct -> JSON []byte
p := Persona{Nombre: "Ana", Edad: 28}
data, err := json.Marshal(p)
// data = {"nombre":"Ana","edad":28}

// MarshalIndent: struct -> JSON legible
data, err := json.MarshalIndent(p, "", "  ")

// Unmarshal: JSON -> struct
var p2 Persona
err = json.Unmarshal(data, &p2)

// Decodificar desde io.Reader (p. ej., http.Response.Body)
dec := json.NewDecoder(resp.Body)
err = dec.Decode(&p2)

// Codificar a io.Writer
enc := json.NewEncoder(os.Stdout)
enc.SetIndent("", "  ")
enc.Encode(p)

// Tipo dinámico
var cualquier cosa any
json.Unmarshal(data, &cualquiercosa)
m := cualquiercosa.(map[string]any) // type assertion
```

**Reglas de Marshal/Unmarshal:**
- Solo campos exportados (mayúscula inicial).
- Tags `json:"nombre"` cambian el nombre en el JSON.
- `omitempty` omite el campo si tiene cero-value.
- `-` (guion) excluye el campo completamente.
- Los campos se asignan por nombre de tag o, si no hay tag, por nombre del campo
  (case-insensitive).

### 9.3 `net/http`

```go
// Servidor HTTP mínimo
http.HandleFunc("/saludo", func(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hola, %s", r.URL.Query().Get("nombre"))
})
http.ListenAndServe(":8080", nil)

// Cliente HTTP
client := &http.Client{Timeout: 10 * time.Second}

resp, err := client.Get("https://api.example.com/datos")
if err != nil { ... }
defer resp.Body.Close()

body, err := io.ReadAll(resp.Body)

// POST con JSON
payload := map[string]string{"clave": "valor"}
data, _ := json.Marshal(payload)
resp, err := client.Post(url, "application/json", bytes.NewReader(data))

// Petición con headers y contexto
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
req.Header.Set("Authorization", "Bearer "+token)
resp, err := client.Do(req)

// Usar el cliente por defecto (paquete http)
resp, err := http.Get(url)
```

### 9.4 `io` y `os` — entrada/salida y archivos

```go
// Leer archivo completo (Go 1.16+)
data, err := os.ReadFile("archivo.txt")

// Escribir archivo completo
err := os.WriteFile("archivo.txt", data, 0o644)

// Abrir para lectura
f, err := os.Open("archivo.txt")
defer f.Close()

// Leer línea a línea
scanner := bufio.NewScanner(f)
for scanner.Scan() {
	linea := scanner.Text()
	fmt.Println(linea)
}

// Abrir para escritura (crea si no existe, añade al final)
f, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)

// Copiar entre readers y writers
io.Copy(destino, origen)

// Interfaces fundamentales
var r io.Reader     // Read(p []byte) (n int, err error)
var w io.Writer     // Write(p []byte) (n int, err error)
var c io.Closer     // Close() error
```

### 9.5 `log/slog` — logging estructurado (Go 1.21+)

```go
// Logger de texto
logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

// Logger JSON
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

// Niveles
logger.Debug("detalle", "clave", valor)
logger.Info("inicio", "puerto", 8080)
logger.Warn("reintentando", "intento", 3)
logger.Error("falló conexión", "err", err)

// Con contexto
logger.InfoContext(ctx, "petición procesada", "duración", d)

// Logger por defecto (paquete slog)
slog.Info("mensaje", "tag", "valor")
```

### 9.6 `time`

```go
ahora := time.Now()                   // hora local
utc := time.Now().UTC()               // UTC
futuro := ahora.Add(24 * time.Hour)   // +1 día

// Formatear y parsear (¡referencia fija de Go!)
// La referencia es: Mon Jan 2 15:04:05 MST 2006  (hora de nacimiento de Go)
str := ahora.Format("2006-01-02 15:04:05")     // "2026-03-15 10:30:00"
t, err := time.Parse("2006-01-02", "2026-03-15")

// Duración
d := 5 * time.Second
time.Sleep(d)

// Ticker (acción periódica)
ticker := time.NewTicker(15 * time.Second)
defer ticker.Stop()
for range ticker.C { ... }

// Timer (acción diferida, una vez)
timer := time.NewTimer(5 * time.Second)
<-timer.C

// Timeout con context
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()
```

### 9.7 `strings` y `strconv`

```go
// strings
strings.Contains("hola mundo", "mundo")     // true
strings.HasPrefix("archivo.go", "archivo")   // true
strings.Split("a,b,c", ",")                  // ["a", "b", "c"]
strings.Join([]string{"a", "b"}, "-")         // "a-b"
strings.TrimSpace("  hola  ")                 // "hola"
strings.ToLower("HOLA")                       // "hola"
strings.ReplaceAll("a-b-c", "-", "/")         // "a/b/c"

// strconv: convertir strings a/desde números
n, _ := strconv.Atoi("42")                   // string -> int
s := strconv.Itoa(42)                        // int -> string
f, _ := strconv.ParseFloat("3.14", 64)       // string -> float64
s = strconv.FormatFloat(3.14, 'f', 2, 64)    // float64 -> string
b, _ := strconv.ParseBool("true")            // string -> bool
```

### 9.8 `flag` — argumentos de línea de comandos

```go
var (
	port    = flag.Int("port", 8080, "puerto del servidor")
	verbose = flag.Bool("verbose", false, "modo detallado")
	config  = flag.String("config", "config.toml", "ruta de configuración")
)
flag.Parse()

fmt.Println("puerto:", *port)
fmt.Println("config:", *config)
```

### 9.9 `embed` — embeber archivos en el binario (Go 1.16+)

```go
import _ "embed"

//go:embed plantilla.html
var plantilla string

//go:embed static/*
var staticFiles embed.FS
```

### 9.10 `sort` y `slices` (Go 1.21+)

```go
nums := []int{3, 1, 4, 1, 5}
sort.Ints(nums)

strs := []string{"z", "a", "m"}
sort.Strings(strs)

// Ordenar structs por campo
type Persona struct{ Nombre string; Edad int }
personas := []Persona{{"Zoe", 30}, {"Ana", 25}}
sort.Slice(personas, func(i, j int) bool {
	return personas[i].Nombre < personas[j].Nombre
})

// Con slices (Go 1.21+, más limpio)
slices.SortFunc(personas, func(a, b Persona) int {
	return cmp.Compare(a.Nombre, b.Nombre)
})
```

---

## 10. Módulos y dependencias

Un **módulo** Go es un conjunto de paquetes versionados juntos. Se define en el
archivo `go.mod` en la raíz del proyecto:

```
module github.com/usuario/proyecto

go 1.22

require (
	github.com/gin-gonic/gin v1.9.1
	golang.org/x/sync v0.5.0
)
```

### 10.1 Comandos esenciales

| Comando | Qué hace |
|---|---|
| `go mod init github.com/usuario/proyecto` | Crea `go.mod` en el directorio actual. |
| `go get pkg@version` | Añade (o actualiza) una dependencia. |
| `go get -u ./...` | Actualiza todas las dependencias a la última versión menor. |
| `go mod tidy` | Añade dependencias que usas, quita las que no, rellena `go.sum`. |
| `go mod download` | Descarga todas las dependencias a la caché local. |
| `go mod why pkg` | ¿Por qué este módulo necesita esa dependencia? |
| `go mod graph` | Muestra el grafo completo de dependencias. |

`go.sum` contiene checksums criptográficos de cada dependencia. **Se commitea**
siempre. Garantiza que todos los desarrolladores y CI usen exactamente las
mismas versiones.

### 10.2 Versiones y versionado semántico

```
github.com/foo/bar v1.2.3
                   │ │ │
                   │ │ └─ patch (bug fixes, retrocompatibles)
                   │ └─── minor (features, retrocompatibles)
                   └───── major (breaking changes, incompatibles)
```

Go usa **Minimum Version Selection** (MVS): no elige la versión más reciente,
sino la **mínima** que satisface todos los `require` del grafo. Esto es
determinista y evita el "dependency hell".

### 10.3 `GOPROXY` y repos privados

Por defecto Go descarga módulos a través de `proxy.golang.org`. Para repos
privados:

```powershell
go env -w GOPRIVATE=github.com/mi-empresa/*
go env -w GONOSUMDB=github.com/mi-empresa/*
```

### 10.4 Workspaces (`go.work`) — Go 1.18+

Cuando editas varios módulos a la vez (p. ej., un servicio y su biblioteca
compartida), un workspace te permite trabajar en ambos sin `replace` en
`go.mod`:

```
go 1.22

use (
	./servicio
	./libreria
)
```

El comando `go run`, `go build`, etc., desde la raíz del workspace resuelven
contra el código local de todos los módulos listados.

---

## 11. Testing

### 11.1 Test unitario básico

Los tests viven en archivos `*_test.go` junto al código, en el mismo paquete:

```go
// calculadora.go
package calc

func Suma(a, b int) int { return a + b }

// calculadora_test.go
package calc

import "testing"

func TestSuma(t *testing.T) {
	resultado := Suma(2, 3)
	if resultado != 5 {
		t.Errorf("Suma(2, 3) = %d; se esperaba 5", resultado)
	}
}
```

`t.Errorf` marca el test como fallido pero **continúa**. `t.Fatalf` marca el
fallo y **detiene** ese test inmediatamente. `t.Logf` imprime solo si el test
falla o con `-v`.

### 11.2 Tests de tabla (table-driven)

Es el patrón idiomático de Go. Separas los datos de prueba de la lógica:

```go
func TestSuma(t *testing.T) {
	casos := []struct {
		nombre string
		a, b   int
		want   int
	}{
		{"positivos", 2, 3, 5},
		{"cero", 0, 5, 5},
		{"negativos", -1, -1, -2},
		{"grandes", 1_000_000, 2_000_000, 3_000_000},
	}

	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			got := Suma(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("Suma(%d, %d) = %d; want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
```

Con `t.Run` cada caso es un subtest independiente. Puedes ejecutar un subtest
concreto: `go test -run TestSuma/positivos`.

### 11.3 Mocking y `testify`

Go favorece **interfaces** para hacer mocking sin dependencias externas. Sin
embargo, `testify` (la biblioteca de testing más popular) ofrece `assert` y
`mock`:

```go
import "github.com/stretchr/testify/assert"

func TestSuma(t *testing.T) {
	assert.Equal(t, 5, Suma(2, 3))
	assert.NotNil(t, resultado)
	assert.NoError(t, err)
}
```

### 11.4 Ejecutar tests

```powershell
go test ./...                        # todos los paquetes
go test -v ./...                     # verbose
go test -run TestSuma ./calculadora  # filtrar por nombre
go test -count=1 ./...               # desactivar caché
go test -race ./...                  # detector de data races
go test -cover ./...                 # % de cobertura
go test -coverprofile=c.out ./...    # perfil de cobertura
go tool cover -html=c.out           # informe visual de cobertura
go test -bench=. ./...               # benchmarks (ver §11.5)
go test -fuzz=FuzzX ./...            # fuzzing
go test -timeout 30s ./...           # timeout global
go test -short ./...                 # saltar tests largos
```

### 11.5 Benchmarks

Los benchmarks miden rendimiento. Viven en `*_test.go` con firma
`func BenchmarkXxx(b *testing.B)`:

```go
func BenchmarkSuma(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Suma(1_000_000, 2_000_000)
	}
}
```

`b.N` se ajusta automáticamente para que el benchmark dure ~1 segundo:

```powershell
go test -bench=. ./...
go test -bench=Suma -benchmem ./...    # incluye allocs/op y bytes/op
go test -bench=. -count=5 ./...        # repetir para estabilidad
```

### 11.6 Fuzzing (Go 1.18+)

El fuzzer genera entradas aleatorias para encontrar bugs:

```go
func FuzzSuma(f *testing.F) {
	f.Add(2, 3)            // semilla
	f.Fuzz(func(t *testing.T, a, b int) {
		resultado := Suma(a, b)
		if resultado < a {  // overflow?
			t.Skip()
		}
	})
}
```

```powershell
go test -fuzz=FuzzSuma -fuzztime=30s ./...
```

---

## 12. Compilación y cross-compilación

### 12.1 Compilar

```powershell
go build .                      # binario con nombre del directorio
go build -o mi-app.exe .       # nombre de salida personalizado
go build -o mi-app.exe ./cmd/server  # compilar un paquete concreto
```

### 12.2 Binario estático

Para un binario que no dependa de libc (portátil, ideal para contenedores
`scratch` y despliegues):

```powershell
$env:CGO_ENABLED = "0"
go build -o mi-app .
```

```bash
CGO_ENABLED=0 go build -o mi-app .
```

Con `CGO_ENABLED=0`, Go usa una implementación Go pura de DNS, crypto, etc.
El binario resultante depende solo del kernel del SO.

### 12.3 Cross-compilación

Desde cualquier SO produces binarios para cualquier otro con solo dos variables:

```powershell
# Para Windows desde PowerShell
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -o mi-app-linux .
```

```bash
# Para Linux/macOS
GOOS=linux   GOARCH=amd64   go build -o mi-app-linux   .
GOOS=linux   GOARCH=arm64   go build -o mi-app-arm64   .
GOOS=windows GOARCH=amd64   go build -o mi-app.exe     .
GOOS=darwin  GOARCH=amd64   go build -o mi-app-intel   .   # Mac Intel
GOOS=darwin  GOARCH=arm64   go build -o mi-app-silicon .   # Mac M1/M2
```

Pares habituales: `linux/amd64`, `linux/arm64`, `windows/amd64`,
`darwin/amd64`, `darwin/arm64`. Lista completa: `go tool dist list`.

### 12.4 `-ldflags` — flags del linker

Inyectar valores en tiempo de compilación (versión, commit, fecha):

```go
package main

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	fmt.Printf("v%s (%s) %s\n", Version, Commit, Date)
}
```

```powershell
go build -ldflags "-s -w -X main.Version=1.0.0 -X main.Commit=$(git rev-parse --short HEAD)" .
```

- `-s`: elimina tabla de símbolos (sin efecto en rendimiento; reduce tamaño).
- `-w`: elimina info de depuración DWARF (ídem).
- `-X paquete.Variable=valor`: reescribe el valor de una variable string.

Tamaño típico: un binario Go sencillo ocupa ~4-8 MB con `-ldflags "-s -w"`,
~8-12 MB sin flags.

### 12.5 Compresión extra (opcional)

```bash
upx --best --lzma mi-app
```

Reduce el binario ~60-70 %. Precaución: algunos antivirus marcan binarios
comprimidos con UPX.

---

## 13. Herramientas y buenas prácticas

### 13.1 Formateo

`gofmt` formatea el código de forma canónica. No hay debate de estilo: el
resultado de `gofmt` **es** el estilo Go.

```powershell
gofmt -w .                # formatea todos los .go del directorio
gofmt -l .                # lista archivos que cambiarían (sin modificar)
go fmt ./...              # alias de gofmt -w (sobre el módulo)
```

VS Code + extensión Go ejecuta `gofmt` automáticamente al guardar.

### 13.2 Análisis estático

```powershell
go vet ./...               # incluido en Go: detecta bugs comunes
```

`go vet` detecta: `fmt.Printf` con argumentos incorrectos, `copylocks`
(estructuras que no se deben copiar), shadowing de variables, `unreachable code`.

**golangci-lint** (metalinter, muy recomendado):

```powershell
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run
golangci-lint run --enable-all  # con todos los linters
```

Incluye decenas de checks: `errcheck`, `staticcheck`, `govet`, `ineffassign`,
`bodyclose`, `gosec` (seguridad), etc.

### 13.3 Depuración con `dlv`

```powershell
go install github.com/go-delve/delve/cmd/dlv@latest
dlv debug .              # depurar desde el inicio
dlv exec ./mi-app        # depurar un binario ya compilado
```

Desde VS Code: punto de ruptura en el gutter > F5.

### 13.4 Perfilado

```go
import _ "net/http/pprof"   // añade rutas /debug/pprof/

go func() {
	http.ListenAndServe("localhost:6060", nil)
}()
```

Luego: `go tool pprof http://localhost:6060/debug/pprof/heap` (memoria),
`/profile` (CPU), `/goroutine`, etc.

### 13.5 Checklist antes de commitear

```powershell
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go mod tidy
```

Considera ejecutar esto como hook de pre-commit automático o en CI.

---

## 14. Organización de proyectos Go

Go no impone una estructura de directorios, pero la comunidad ha convergido en
convenciones:

### 14.1 Proyecto pequeño (un comando)

```
mi-proyecto/
├── go.mod
├── main.go
├── config.go
└── handler.go
```

Todo en la raíz, paquete `main`. Ideal para herramientas, scripts, prototipos.

### 14.2 Proyecto mediano (un comando, varios paquetes internos)

```
mi-proyecto/
├── go.mod
├── main.go                # package main, función main
├── config/
│   └── config.go          # package config
├── store/
│   └── store.go           # package store
└── api/
    └── server.go          # package api
```

La raíz es `package main`. El resto son paquetes internos de dominio. No hay
`src/`, `internal/` ni `pkg/` por defecto hasta que el proyecto crece.

### 14.3 Proyecto grande (múltiples comandos, bibliotecas públicas)

```
mi-proyecto/
├── go.mod
├── cmd/
│   ├── server/main.go     # comando servidor
│   └── worker/main.go     # comando worker
├── internal/              # paquetes privados (Go prohíbe importarlos desde fuera del módulo)
│   ├── store/
│   └── service/
├── pkg/                   # bibliotecas públicas para consumidores externos (uso controvertido)
│   └── client/
├── api/                   # definiciones gRPC / OpenAPI
├── test/                  # tests de integración / e2e
└── docs/
```

- `cmd/`: un subdirectorio por cada comando. Cada uno es un `package main`.
- `internal/`: el compilador impide que otros módulos importen estos paquetes.
  Es el mecanismo de encapsulación de Go.
- `pkg/`: paquetes que sí pueden ser importados por módulos externos. Mucha
  gente no usa `pkg/` y deja los paquetes públicos a la raíz.
- `go.mod` siempre en la raíz del repo.

### 14.4 Nombrado de paquetes

- **Todo en minúsculas, sin guiones ni underscores.** `httpclient`, no
  `http-client` ni `http_client`.
- El nombre del paquete debe ser corto, descriptivo y **distinto del nombre del
  directorio si el directorio ya da contexto** (esto es controvertido; la stdlib
  lo hace: `package http` en `net/http/`).
- Evita nombres genéricos: `util`, `common`, `helper`, `misc`. Si un paquete se
  llama `util`, probablemente deberías dividirlo.

---

## 15. Recursos oficiales

| Recurso | Enlace | Propósito |
|---|---|---|
| Tour de Go | https://go.dev/tour/ | Aprender sintaxis en el navegador (2-3 h). Empieza aquí. |
| Effective Go | https://go.dev/doc/effective_go | Cómo escribir Go idiomático. |
| Especificación | https://go.dev/ref/spec | La especificación formal (sorprendentemente legible). |
| Stdlib | https://pkg.go.dev/std | Documentación de cada paquete estándar. |
| Go Blog | https://go.dev/blog/ | Artículos oficiales, notas de versión. |
| Go by Example | https://gobyexample.com/ | Ejemplos autocontenidos de cada concepto. |
| Awesome Go | https://github.com/avelino/awesome-go | Lista curada de bibliotecas. |
| Learn Go with Tests | https://quii.gitbook.io/learn-go-with-tests | Aprender Go guiado por tests (muy didáctico). |
| 100 Go Mistakes | https://100go.co/ | Errores comunes y cómo evitarlos. |

### Ruta de aprendizaje sugerida

1. Tour de Go (básico + métodos/interfaces + concurrencia).
2. Lee esta guía por encima para situar cada concepto.
3. Escribe algo real: un CLI, una API HTTP pequeña, una herramienta.
4. Vuelve a §8 (concurrencia) cuando tu programa lo pida: es donde Go brilla.
5. Aplica §13 y §14 cuando el proyecto crezca y necesites orden.

---

*Guía autocontenida. No depende de ningún proyecto concreto.*
