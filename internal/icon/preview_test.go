package icon

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"image/color"

	xdraw "golang.org/x/image/draw"
)

// TestPreview vuelca una hoja de contacto con los tres estados a varios
// tamaños, para revisión visual. No es una aserción: es una herramienta de
// desarrollo, así que solo corre si se le dice explícitamente dónde escribir:
//
//	HYGEIA_ICON_PREVIEW_DIR=/tmp go test ./internal/icon/ -run TestPreview
func TestPreview(t *testing.T) {
	outDir := os.Getenv("HYGEIA_ICON_PREVIEW_DIR")
	if outDir == "" {
		t.Skip("define HYGEIA_ICON_PREVIEW_DIR para generar la hoja de contacto")
	}

	mark, err := png.Decode(bytes.NewReader(hygeiaMark))
	if err != nil {
		t.Fatal(err)
	}
	states := []struct {
		name string
		c    [3]uint8
		dim  bool
	}{
		{"connected", [3]uint8{0x2E, 0x9E, 0x5B}, false},
		{"local_error", [3]uint8{0xC9, 0x3F, 0x3F}, false},
		{"unconfigured", [3]uint8{0x9A, 0xA0, 0xA6}, true},
	}
	previewSizes := []int{16, 32, 48}

	// Hoja: filas = estados, columnas = tamaños, todo ampliado x4 con vecino
	// más cercano para ver el pixelado real.
	const zoom, cell = 4, 48
	sheet := image.NewRGBA(image.Rect(0, 0, cell*zoom*len(previewSizes), cell*zoom*len(states)))
	for row, s := range states {
		for col, size := range previewSizes {
			img := compose(mark, size, rgba(s.c), s.dim)
			x0, y0 := col*cell*zoom, row*cell*zoom
			dst := image.Rect(x0, y0, x0+size*zoom, y0+size*zoom)
			xdraw.NearestNeighbor.Scale(sheet, dst, img, img.Bounds(), xdraw.Over, nil)
		}
	}
	out := filepath.Join(outDir, "hygeia-icons-preview.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sheet); err != nil {
		t.Fatal(err)
	}
	t.Logf("hoja de contacto escrita en %s", out)
}

func rgba(c [3]uint8) color.RGBA {
	return color.RGBA{R: c[0], G: c[1], B: c[2], A: 0xFF}
}
