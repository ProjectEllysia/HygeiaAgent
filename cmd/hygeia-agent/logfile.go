package main

import (
	"os"
	"sync"
)

// maxLogBytes es el tamaño al que se rota el fichero de log.
//
// El README §5 declara innegociable que nada crezca sin límite, y eso se
// respetó para el buffer en disco y para el logring en memoria, pero no para
// el propio fichero de log: se abría en modo añadir y no se rotaba nunca. A
// un heartbeat cada 15 s eran unos 350 MB al año por equipo, y en un servidor
// con la partición raíz pequeña eso llega a llenar el disco — que es
// precisamente de lo que el agente está ahí para avisar.
const maxLogBytes = 5 << 20 // 5 MB

// rotatingWriter escribe en un fichero y, cuando supera maxBytes, lo renombra
// a <ruta>.1 y empieza uno nuevo.
//
// Se conserva UN solo fichero anterior. Es suficiente para ver qué pasaba
// antes de un problema, que es para lo que se miran estos logs, y evita tener
// que decidir políticas de retención, compresión o numeración — todo lo que
// convierte una rotación en una dependencia externa. Si algún día hace falta
// más, ahí está lumberjack.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	size     int64
}

// newRotatingWriter abre (o crea) el fichero en modo añadir. Devuelve error
// si no se puede abrir, para que el caller caiga a stderr.
func newRotatingWriter(path string, maxBytes int64) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// El tamaño de partida es el que ya tuviera el fichero: si no, tras un
	// reinicio del servicio la cuenta empezaría de cero y el fichero podría
	// crecer sin tope entre reinicios.
	size := int64(0)
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	w.f = f
	w.size = size
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxBytes {
		// Un fallo al rotar no debe perder la línea que se está escribiendo:
		// se registra el intento y se sigue escribiendo en el fichero actual,
		// que a lo sumo crece un poco por encima del tope hasta el próximo
		// intento.
		_ = w.rotateLocked()
	}

	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	// Renombrar pisa el .1 anterior, que es justo lo que se quiere: dos
	// ficheros como mucho, sin acumular histórico.
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		// Si el renombrado falla (fichero bloqueado por otro proceso en
		// Windows, permisos), hay que reabrir igualmente: quedarse sin
		// destino de log sería peor que un fichero grande.
		if openErr := w.open(); openErr != nil {
			return openErr
		}
		return err
	}
	return w.open()
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
