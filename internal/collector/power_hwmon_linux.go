package collector

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// defaultHwmonBasePath es la raíz de los sensores de hardware en Linux.
// Variable para que los tests la sustituyan por un árbol temporal.
var defaultHwmonBasePath = "/sys/class/hwmon"

var hwmonPowerInputFile = regexp.MustCompile(`^power(\d+)_input$`)

// hwmonRecognizedDrivers son los drivers de hwmon cuyo powerN_input se sabe
// que representa el consumo de TODA la máquina —una fuente de alimentación o
// el controlador de gestión de la placa—, no el de un componente aislado.
//
// Deliberadamente corta y pensada para crecer: un sensor de un driver que no
// está aquí se ignora y queda en el log con su nombre y su ruta (P09), que es
// de donde debe salir la próxima entrada, no de una heurística sobre
// cualquier power*_input que aparezca.
var hwmonRecognizedDrivers = map[string]bool{
	"pmbus":       true, // fuentes de servidor compatibles con PMBus
	"ibmpowernv":  true, // controlador de gestión de IBM POWER
	"corsair-psu": true, // fuentes de sobremesa Corsair con telemetría USB
}

// hwmonProvider lee sensores de potencia de /sys/class/hwmon (P09). A
// diferencia de RAPL o las GPU, hwmon es heterogéneo: un powerN_input puede
// ser el consumo total de la placa o el de un solo raíl, y la única
// información de procedencia disponible es el nombre del driver. Por eso
// solo se usan los reconocidos explícitamente en hwmonRecognizedDrivers, y
// se toma el PRIMERO que aparece: al representar la máquina entera, no hay
// nada que sumar entre ellos.
type hwmonProvider struct {
	basePath string
	log      *slog.Logger

	warnedUnknown sync.Map // devicePath -> struct{}, para avisar una sola vez por sensor
}

func newHwmonProvider(log *slog.Logger) *hwmonProvider {
	return &hwmonProvider{basePath: defaultHwmonBasePath, log: log}
}

func (p *hwmonProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	dirs, err := os.ReadDir(p.basePath)
	if err != nil {
		return nil, nil
	}

	for _, dir := range dirs {
		devicePath := filepath.Join(p.basePath, dir.Name())
		entries, err := os.ReadDir(devicePath)
		if err != nil {
			continue
		}

		powerFile := ""
		for _, e := range entries {
			if hwmonPowerInputFile.MatchString(e.Name()) {
				powerFile = e.Name()
				break
			}
		}
		if powerFile == "" {
			continue // este hwmon no publica potencia (temperatura, ventilador...)
		}

		driver := readTrimmedFile(filepath.Join(devicePath, "name"))
		if !hwmonRecognizedDrivers[driver] {
			p.warnUnrecognized(devicePath, driver)
			continue
		}

		microWatts, ok := parseFloatTrimmed(readTrimmedFile(filepath.Join(devicePath, powerFile)))
		if !ok {
			continue
		}
		return &payload.PowerMetrics{Watts: microWatts / 1e6, Estimated: false, Source: "hwmon:" + driver}, nil
	}
	return nil, nil
}

func (p *hwmonProvider) warnUnrecognized(devicePath, driver string) {
	if _, loaded := p.warnedUnknown.LoadOrStore(devicePath, struct{}{}); loaded {
		return
	}
	label := readTrimmedFile(filepath.Join(devicePath, "power1_label"))
	p.log.Info("sensor de potencia de hwmon ignorado: driver no reconocido",
		"driver", driver, "label", label, "path", devicePath)
}

func readTrimmedFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func parseFloatTrimmed(v string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
