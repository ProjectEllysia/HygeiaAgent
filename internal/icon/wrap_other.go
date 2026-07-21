//go:build !windows

package icon

import "image"

// wrap devuelve el PNG del tamaño mayor. En Linux (StatusNotifierItem/AppIndicator)
// y macOS (NSStatusItem) la bandeja espera un PNG y reescala ella misma, así
// que se le da el más grande disponible.
func wrap(imgs []*image.RGBA) []byte {
	if len(imgs) == 0 {
		return nil
	}
	best := imgs[0]
	for _, img := range imgs[1:] {
		if img.Bounds().Dx() > best.Bounds().Dx() {
			best = img
		}
	}
	return encodePNG(best)
}
