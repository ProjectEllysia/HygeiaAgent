package collector

import (
	"context"
	"sync"
	"time"

	gpsnet "github.com/shirou/gopsutil/v4/net"
	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type NetworkCollector struct {
	mu        sync.Mutex
	prev      map[string]gpsnet.IOCountersStat
	prevTime  time.Time
}

func NewNetwork() Collector { return &NetworkCollector{prev: make(map[string]gpsnet.IOCountersStat)} }

func (c *NetworkCollector) Name() string { return "network" }

// Collect convierte los contadores absolutos de gopsutil (bytes/paquetes
// acumulados desde el arranque) en TASA por segundo, comparando contra el
// snapshot anterior del propio collector (README §3). El primer ciclo no
// tiene tasa y se omiten esas interfaces (no se falsea el dato).
func (c *NetworkCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	counters, err := gpsnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return err
	}

	now := time.Now()
	c.mu.Lock()
	prev := c.prev
	prevT := c.prevTime
	c.prev = make(map[string]gpsnet.IOCountersStat, len(counters))
	for _, io := range counters {
		c.prev[io.Name] = io
	}
	c.prevTime = now
	c.mu.Unlock()

	elapsed := now.Sub(prevT).Seconds()
	if elapsed <= 0 || len(prev) == 0 {
		return nil // primer ciclo: nada que diferenciar
	}

	var out []payload.NetworkMetrics
	for _, io := range counters {
		// Filtramos lo0/loopback para no añadir ruido al heartbeat.
		if io.Name == "lo" {
			continue
		}
		old, ok := prev[io.Name]
		if !ok {
			continue
		}
		out = append(out, payload.NetworkMetrics{
			Iface:         io.Name,
			RxBytesPerSec: rate(io.BytesRecv, old.BytesRecv, elapsed),
			TxBytesPerSec: rate(io.BytesSent, old.BytesSent, elapsed),
			ErrIn:         io.Errin,
			ErrOut:        io.Errout,
		})
	}
	if len(out) > 0 {
		m.Network = out
	}
	return nil
}

// rate calcula delta/segundo manejando el wrap-around (reinicio de
// contadores, reboot de interfaz...). Si el contador bajó, tomamos el
// valor absoluto actual como delta (reinicio de interfaz).
func rate(cur, old uint64, elapsed float64) float64 {
	var d uint64
	if cur >= old {
		d = cur - old
	} else {
		d = cur
	}
	return float64(d) / elapsed
}