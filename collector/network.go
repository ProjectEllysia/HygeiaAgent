package collector

import (
	"context"

	"github.com/ProjectEllysia/Ellysia-Hygeia/payload"
)

type NetworkCollector struct{}

func NewNetwork() Collector { return &NetworkCollector{} }

func (c *NetworkCollector) Name() string { return "network" }

func (c *NetworkCollector) Collect(ctx context.Context, m *payload.Metrics) error {
	// TODO: implementar con github.com/shirou/gopsutil/v4/net:
	//   net.IOCounters(true) -> []IOCountersStat: .Name, .BytesRecv, .BytesSent,
	//                          .Errin, .Errout, .Dropin, .Dropout
	//   OJO: hay que convertir acumulado -> tasa (delta/tiempo) guardando el
	//   snapshot anterior en el collector (campos en el struct).
	return nil
}
