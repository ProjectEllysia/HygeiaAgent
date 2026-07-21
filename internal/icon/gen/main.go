// Command gen genera hygeia.ico a partir de resources/hygeia/Hygeia-DarkGreen-BgN.png
// (el mismo mark de marca que ya usa el tray, internal/icon/hygeia-mark.png)
// para el icono de los propios .exe del agente y del tray — sin esto,
// Explorer/Alt+Tab/Administrador de tareas muestran el icono genérico de Go.
//
// Uso (solo hace falta rerun si cambia el arte fuente):
//
//	go run ./internal/icon/gen
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"

	xdraw "golang.org/x/image/draw"
)

// sizes: 16/32/48 son los tamaños clásicos de barra de tareas/Explorer; 256
// es el que usa Windows en las vistas de icono grande y en el instalador.
var sizes = []int{16, 32, 48, 256}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	srcPath := filepath.Join(repoRoot, "resources", "hygeia", "Hygeia-DarkGreen-BgN.png")

	f, err := os.Open(srcPath)
	if err != nil {
		log.Fatalf("gen: abriendo %s: %v", srcPath, err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatalf("gen: decodificando %s: %v", srcPath, err)
	}

	cropped := cropToOpaque(src)

	imgs := make([]*image.RGBA, 0, len(sizes))
	for _, s := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, s, s))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), cropped, cropped.Bounds(), xdraw.Over, nil)
		imgs = append(imgs, dst)
	}

	icoPath := filepath.Join(repoRoot, "internal", "icon", "hygeia.ico")
	if err := os.WriteFile(icoPath, wrapICO(imgs), 0o644); err != nil {
		log.Fatalf("gen: escribiendo %s: %v", icoPath, err)
	}
	log.Printf("gen: %s escrito (%d tamaños)", icoPath, len(sizes))

	genWizardBanner(repoRoot)
}

// wizardW/H mantienen el ratio de aspecto clásico del panel lateral del
// wizard de Inno Setup (164x314 ≈ 0.522), a 2x para que se vea nítido en
// pantallas de alta densidad. Inno reescala esta imagen al tamaño real del
// panel en tiempo de compilación (WizardImageStretch) — como el ratio ya
// coincide, ese reescalado es uniforme y no deforma el laurel (a diferencia
// de darle a Inno la imagen casi cuadrada de origen, que sale estirada a
// óvalo, o de desactivar el stretch, que solo muestra una esquina recortada
// — ambos bugs reales encontrados al probar el instalador de verdad).
const (
	wizardW = 328
	wizardH = 628
)

// genWizardBanner genera el panel lateral del wizard: el laurel completo
// (fondo blanco, resources/hygeia/Ellysia-Hygeia-DarkGreen-BgW.png)
// recortado a su contenido, escalado para caber por ancho y centrado sobre
// un lienzo blanco del ratio de aspecto correcto.
func genWizardBanner(repoRoot string) {
	srcPath := filepath.Join(repoRoot, "resources", "hygeia", "Ellysia-Hygeia-DarkGreen-BgW.png")
	f, err := os.Open(srcPath)
	if err != nil {
		log.Fatalf("gen: abriendo %s: %v", srcPath, err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatalf("gen: decodificando %s: %v", srcPath, err)
	}

	cropped := cropToNonWhite(src)

	// Escalar por ancho (el panel es más alto que ancho respecto al origen
	// casi cuadrado) y centrar verticalmente.
	scale := float64(wizardW) / float64(cropped.Bounds().Dx())
	scaledH := int(float64(cropped.Bounds().Dy()) * scale)
	scaledRect := image.Rect(0, 0, wizardW, scaledH)
	scaled := image.NewRGBA(scaledRect)
	xdraw.CatmullRom.Scale(scaled, scaledRect, cropped, cropped.Bounds(), xdraw.Src, nil)

	canvas := image.NewRGBA(image.Rect(0, 0, wizardW, wizardH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	offsetY := (wizardH - scaledH) / 2
	draw.Draw(canvas, image.Rect(0, offsetY, wizardW, offsetY+scaledH), scaled, image.Point{}, draw.Over)

	outPath := filepath.Join(repoRoot, "internal", "icon", "hygeia-wizard.png")
	out, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("gen: creando %s: %v", outPath, err)
	}
	defer out.Close()
	if err := png.Encode(out, canvas); err != nil {
		log.Fatalf("gen: codificando %s: %v", outPath, err)
	}
	log.Printf("gen: %s escrito (%dx%d)", outPath, wizardW, wizardH)
}

// cropToNonWhite recorta a la bounding box del contenido en una imagen de
// fondo BLANCO (sin canal alfa útil, a diferencia de cropToOpaque): cualquier
// píxel que no sea (casi) blanco cuenta como contenido.
func cropToNonWhite(img image.Image) image.Image {
	const threshold = 0xF000 // canales en escala 0-0xFFFF; ~98% de blanco
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r < threshold || g < threshold || bl < threshold {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	rect := image.Rect(minX, minY, maxX+1, maxY+1)
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

// cropToOpaque recorta a la bounding box de píxeles no transparentes: el PNG
// fuente trae margen extra alrededor del mark (igual que el comentario de
// internal/icon/icon.go describe para hygeia-mark.png), y conservarlo
// dejaría la figura innecesariamente pequeña dentro del icono.
func cropToOpaque(img image.Image) image.Image {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	rect := image.Rect(minX, minY, maxX+1, maxY+1)
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

// wrapICO empaqueta los tamaños en un ICO multi-imagen (mismo formato que
// internal/icon/wrap_windows.go: PNG embebido tal cual, soportado desde
// Vista). Duplicado a propósito en vez de importar ese paquete: es un
// generador de un solo uso, no vale la pena exportar la función interna del
// tray por esto.
func wrapICO(imgs []*image.RGBA) []byte {
	type entry struct {
		img  *image.RGBA
		data []byte
	}
	entries := make([]entry, 0, len(imgs))
	for _, img := range imgs {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			log.Fatalf("gen: codificando PNG: %v", err)
		}
		entries = append(entries, entry{img: img, data: buf.Bytes()})
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w := e.img.Bounds().Dx()
		h := e.img.Bounds().Dy()
		buf.WriteByte(byte(w % 256))
		buf.WriteByte(byte(h % 256))
		buf.WriteByte(0)
		buf.WriteByte(0)
		binary.Write(&buf, binary.LittleEndian, uint16(1))
		binary.Write(&buf, binary.LittleEndian, uint16(32))
		binary.Write(&buf, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes()
}
