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
	"time"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

// defaultRAPLBasePath es la raíz del subsistema powercap en Linux. Variable
// (no const) para que los tests la sustituyan por un directorio temporal.
var defaultRAPLBasePath = "/sys/class/powercap"

// raplTopLevelDomain empareja los dominios de PRIMER nivel (intel-rapl:N) y
// excluye sus subdominios (intel-rapl:N:M — núcleo gráfico integrado, DRAM).
// Los subdominios ya están contenidos en la energía de su padre: sumarlos
// contaría la misma energía dos veces dentro de la propia CPU (P12).
var raplTopLevelDomain = regexp.MustCompile(`^intel-rapl:\d+$`)

// discoverRAPLDomains lista los directorios de dominio de primer nivel bajo
// basePath. Vive aparte de raplProvider para que DiagnosePower (P08) pueda
// reutilizarla sin necesitar un proveedor con estado completo.
func discoverRAPLDomains(basePath string) []string {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil
	}
	var domains []string
	for _, e := range entries {
		if raplTopLevelDomain.MatchString(e.Name()) {
			domains = append(domains, filepath.Join(basePath, e.Name()))
		}
	}
	return domains
}

type raplDomainState struct {
	path      string
	maxRange  uint64 // 0 = todavía no leído; se lee una sola vez (P06)
	prevValue uint64
	havePrev  bool
}

// maxRangeFor lee max_energy_range_uj una sola vez por dominio: no cambia
// entre lecturas, y consultarlo en cada ciclo sería un fichero más sin
// ningún beneficio.
func (d *raplDomainState) maxRangeFor() (uint64, bool) {
	if d.maxRange != 0 {
		return d.maxRange, true
	}
	v, ok := readUint(filepath.Join(d.path, "max_energy_range_uj"))
	if !ok || v == 0 {
		return 0, false
	}
	d.maxRange = v
	return v, true
}

// raplProvider deriva vatios del contador de energía acumulada que expone
// powercap (P06): energy_uj es energía en microjulios desde que arrancó el
// contador, no potencia, así que hace falta una muestra anterior para
// calcular W = ΔJ/Δs. El primer ciclo tras arrancar el agente no tiene con
// qué comparar y no produce dato — el mismo patrón que NetworkCollector usa
// para sus tasas.
//
// Corrige además el desbordamiento del contador (P07: el fichero se reinicia
// a 0 al llegar a max_energy_range_uj, con valores típicos cada pocas decenas
// de minutos bajo carga) y degrada en silencio, con un aviso distinto según
// el motivo, cuando falta el privilegio de root que exige el kernel desde la
// 5.10 o cuando esta máquina no expone RAPL en absoluto (P08).
type raplProvider struct {
	basePath string
	log      *slog.Logger

	mu       sync.Mutex
	domains  []*raplDomainState
	scanned  bool
	prevTime time.Time

	warnPermissionOnce sync.Once
	warnAbsentOnce     sync.Once
}

func newRAPLProvider(log *slog.Logger) *raplProvider {
	return &raplProvider{basePath: defaultRAPLBasePath, log: log}
}

func (p *raplProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.scanned {
		for _, path := range discoverRAPLDomains(p.basePath) {
			p.domains = append(p.domains, &raplDomainState{path: path})
		}
		p.scanned = true
	}
	if len(p.domains) == 0 {
		p.warnAbsentOnce.Do(func() {
			p.log.Info("RAPL no disponible: esta máquina no expone " + p.basePath + "/intel-rapl:*")
		})
		return nil, nil
	}

	now := time.Now()
	firstCycle := p.prevTime.IsZero()
	elapsed := now.Sub(p.prevTime).Seconds()
	p.prevTime = now

	var total uint64
	var contributed, anyReadable, sawPermission bool
	for _, d := range p.domains {
		cur, err := os.ReadFile(filepath.Join(d.path, "energy_uj"))
		if err != nil {
			if os.IsPermission(err) {
				sawPermission = true
			}
			continue
		}
		anyReadable = true
		val, perr := strconv.ParseUint(strings.TrimSpace(string(cur)), 10, 64)
		if perr != nil {
			continue
		}

		if d.havePrev {
			switch {
			case val >= d.prevValue:
				total += val - d.prevValue
				contributed = true
			default:
				// El contador dio la vuelta. Sin max_energy_range_uj no hay
				// con qué corregirlo: se descarta este dominio en ESTE ciclo
				// (mejor perder una muestra que publicar un pico inventado),
				// no el proveedor entero.
				if maxRange, ok := d.maxRangeFor(); ok {
					total += (maxRange - d.prevValue) + val
					contributed = true
				}
			}
		}
		d.prevValue = val
		d.havePrev = true
	}

	if !anyReadable {
		if sawPermission {
			p.warnPermissionOnce.Do(func() {
				p.log.Info("RAPL presente pero sin privilegio de lectura: energy_uj es 0400 desde Linux 5.10 " +
					"(CVE-2020-8694, PLATYPUS); ejecuta el agente como root, o instálalo como servicio")
			})
		} else {
			p.warnAbsentOnce.Do(func() {
				p.log.Info("RAPL no disponible: los ficheros de energía no se pudieron leer")
			})
		}
		return nil, nil
	}
	if firstCycle || !contributed || elapsed <= 0 {
		return nil, nil
	}

	watts := float64(total) / 1e6 / elapsed
	return &payload.PowerMetrics{Watts: watts, Estimated: true, Source: "rapl"}, nil
}

func readUint(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
