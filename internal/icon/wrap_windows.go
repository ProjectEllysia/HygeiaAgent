package icon

import (
	"bytes"
	"encoding/binary"
	"image"
)

// wrap empaqueta los tamaños en un ICO multi-imagen, que es lo que espera
// Shell_NotifyIcon en Windows. Se incluyen todos los tamaños para que el
// shell elija según el DPI en vez de reescalar uno solo (reescalar line-art
// a 16px da un resultado notablemente peor que rasterizarlo a 16px).
//
// Formato ICO (los PNG van embebidos tal cual, soportado desde Vista):
//
//	ICONDIR    6 bytes   cabecera + nº de imágenes
//	ICONDIRENTRY  16 bytes por imagen
//	datos      los PNG concatenados
func wrap(imgs []*image.RGBA) []byte {
	type entry struct {
		img  *image.RGBA
		data []byte
	}
	entries := make([]entry, 0, len(imgs))
	for _, img := range imgs {
		entries = append(entries, entry{img: img, data: encodePNG(img)})
	}

	var buf bytes.Buffer
	// ICONDIR: reserved=0, type=1 (icono), count.
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	// Los datos empiezan tras la cabecera y todas las entradas del directorio.
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w := e.img.Bounds().Dx()
		h := e.img.Bounds().Dy()
		// En ICONDIRENTRY, 256 se codifica como 0 en un byte. Nuestros
		// tamaños son <= 48, pero la conversión se deja explícita.
		buf.WriteByte(byte(w % 256))
		buf.WriteByte(byte(h % 256))
		buf.WriteByte(0)                                        // colores en paleta: 0 = sin paleta
		buf.WriteByte(0)                                        // reservado
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))  // planos
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits por píxel
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		buf.Write(e.data)
	}
	return buf.Bytes()
}
