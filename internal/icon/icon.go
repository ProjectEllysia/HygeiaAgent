// Package icon genera los iconos de bandeja de los tres estados (§11.2).
//
// El icono es el mark de marca de Hygeia (resources/hygeia) con un punto de
// color superpuesto en la esquina inferior derecha. El mark da identidad; el
// punto es lo que de verdad se lee a tamaño de bandeja, donde el line-art
// del mark se empasta y no distinguiría un estado de otro por sí solo.
package icon

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// hygeiaMark es resources/hygeia/Hygeia-DarkGreen-BgN.png (el que tiene
// fondo transparente), recortado a su bounding box y reescalado a 128x128.
// Se embebe porque go:embed no puede salir del directorio del paquete y
// porque el original (716x766, 118 KB) es innecesariamente grande para un
// icono de 16-48 px.
//
// Lo genera ./gen a partir del arte de resources/. La directiva deja esa
// relación explícita: `go generate ./...` regenera el arte derivado sin que
// nadie tenga que acordarse del comando.
//
//go:generate go run ./gen
//go:embed hygeia-mark.png
var hygeiaMark []byte

// Paleta de estados (§11.2): verde conectado, rojo error local, gris sin
// configurar. Tonos con contraste suficiente para leerse tanto en una barra
// de tareas clara como oscura.
var (
	colorConnected    = color.RGBA{R: 0x2E, G: 0x9E, B: 0x5B, A: 0xFF}
	colorLocalError   = color.RGBA{R: 0xC9, G: 0x3F, B: 0x3F, A: 0xFF}
	colorUnconfigured = color.RGBA{R: 0x9A, G: 0xA0, B: 0xA6, A: 0xFF}
)

// Estados del canal de control. Se repiten aquí como literales en vez de
// importar `control` para que este paquete no dependa de él (el tray sí
// depende de ambos).
const (
	stateConnected  = "connected"
	stateLocalError = "local_error"
	// key_rejected comparte el icono rojo con local_error: son problemas
	// distintos —uno es la credencial y el otro la máquina— pero los dos
	// significan lo mismo de un vistazo, que es para lo que sirve el icono:
	// este activo NO está reportando y hace falta hacer algo. La diferencia
	// la cuenta el texto del menú, que sí tiene sitio para explicarla.
	//
	// Sin esta línea caería en el default y saldría el gris de "sin
	// configurar", que diría justo lo contrario de lo que pasa.
	stateKeyRejected = "key_rejected"
	// "starting" y "unconfigured" comparten el icono gris: en ambos el
	// agente está vivo pero todavía no reporta nada al backend.
)

// Los iconos no cambian nunca, así que se generan una vez por proceso: el
// tray llama a SetIcon en cada refresco de estado.
var (
	once                                sync.Once
	connected, localError, unconfigured []byte
)

func build() {
	mark, err := png.Decode(bytes.NewReader(hygeiaMark))
	if err != nil {
		// El asset viaja embebido en el binario: si no decodifica, el binario
		// está corrupto y no hay recuperación sensata.
		log.Panicf("icon: mark embebido ilegible: %v", err)
	}
	connected = encode(mark, colorConnected, false)
	localError = encode(mark, colorLocalError, false)
	// Sin configurar, además del punto gris, el propio mark se atenúa: el
	// agente está instalado pero inactivo, y eso debe leerse de un vistazo.
	unconfigured = encode(mark, colorUnconfigured, true)
}

// Connected devuelve el icono con punto verde (último heartbeat con éxito).
func Connected() []byte { once.Do(build); return connected }

// LocalError devuelve el icono con punto rojo (no se recolecta, o el buffer
// crece porque el backend no responde).
func LocalError() []byte { once.Do(build); return localError }

// Unconfigured devuelve el icono atenuado con punto gris (instalado pero sin
// dar de alta contra ningún activo).
func Unconfigured() []byte { once.Do(build); return unconfigured }

// ForState mapea un estado del canal de control a su icono.
func ForState(state string) []byte {
	switch state {
	case stateConnected:
		return Connected()
	case stateLocalError, stateKeyRejected:
		return LocalError()
	default:
		return Unconfigured()
	}
}

// sizes son los tamaños que se generan. Windows elige de un ICO multi-tamaño
// según DPI; el resto de plataformas usa el mayor y escala.
var sizes = []int{16, 32, 48}

// compose dibuja el mark reescalado a `size` con el punto de estado encima.
func compose(mark image.Image, size int, dot color.RGBA, dim bool) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))

	// El mark ocupa el lienzo entero: a 16px cada píxel cuenta, y encogerlo
	// para "hacer sitio" al punto solo lo haría más ilegible. El punto se
	// superpone en la esquina, como un badge.
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), mark, mark.Bounds(), xdraw.Over, nil)

	if dim {
		dimImage(dst)
	}
	drawDot(dst, dot)
	return dst
}

// dimImage baja el alfa del mark para el estado "sin configurar".
func dimImage(img *image.RGBA) {
	const factor = 0.45
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := img.PixOffset(x, y)
			// image.RGBA está premultiplicado por alfa, así que escalar los
			// cuatro canales por igual mantiene la premultiplicación válida.
			for c := range 4 {
				img.Pix[i+c] = uint8(float64(img.Pix[i+c]) * factor)
			}
		}
	}
}

// drawDot pinta el punto de estado en la esquina inferior derecha, con un
// borde más oscuro para que no se pierda sobre fondos del mismo tono.
func drawDot(img *image.RGBA, c color.RGBA) {
	size := img.Bounds().Dx()
	// 21% del lado => diámetro del 42%. Suficiente para leerse a 16px (unos
	// 7px de punto) sin tapar la figura del mark.
	radius := float64(size) * 0.21
	cx := float64(size) - radius - 0.5
	cy := float64(size) - radius - 0.5

	// Borde: el mismo color al 55% de luminosidad.
	border := color.RGBA{
		R: uint8(float64(c.R) * 0.55),
		G: uint8(float64(c.G) * 0.55),
		B: uint8(float64(c.B) * 0.55),
		A: 0xFF,
	}
	borderWidth := math.Max(1, float64(size)*0.06)

	for y := range size {
		for x := range size {
			dx := float64(x) - cx
			dy := float64(y) - cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > radius+0.5 {
				continue
			}

			fill := c
			if dist > radius-borderWidth {
				fill = border
			}
			// Antialias en el borde exterior: franja de 1px donde el alfa
			// cae de 1 a 0. Sin esto el punto se ve dentado a 16px.
			alpha := 1.0
			if dist > radius-0.5 {
				alpha = (radius + 0.5) - dist
			}
			blendPixel(img, x, y, fill, alpha)
		}
	}
}

// blendPixel compone `src` (con alfa `a`) sobre el píxel existente usando
// source-over, respetando el premultiplicado de image.RGBA.
func blendPixel(img *image.RGBA, x, y int, src color.RGBA, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	i := img.PixOffset(x, y)
	inv := 1 - a
	img.Pix[i+0] = uint8(float64(src.R)*a + float64(img.Pix[i+0])*inv)
	img.Pix[i+1] = uint8(float64(src.G)*a + float64(img.Pix[i+1])*inv)
	img.Pix[i+2] = uint8(float64(src.B)*a + float64(img.Pix[i+2])*inv)
	img.Pix[i+3] = uint8(float64(src.A)*a + float64(img.Pix[i+3])*inv)
}

// encode genera todos los tamaños y los empaqueta en el formato que espera
// la bandeja de cada plataforma (ICO en Windows, PNG en el resto).
func encode(mark image.Image, dot color.RGBA, dim bool) []byte {
	imgs := make([]*image.RGBA, 0, len(sizes))
	for _, s := range sizes {
		imgs = append(imgs, compose(mark, s, dot, dim))
	}
	return wrap(imgs)
}

// encodePNG serializa una imagen a PNG.
func encodePNG(img *image.RGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		// bytes.Buffer no falla al escribir.
		log.Panicf("icon: codificando PNG: %v", err)
	}
	return buf.Bytes()
}
