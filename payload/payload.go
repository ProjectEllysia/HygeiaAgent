// Package payload define el contrato de ingesta (README §9) como tipos Go.
//
// Es el ÚNICO acoplamiento entre el agente y el backend: quien cambie estos
// tipos cambia la costura. Por eso viven en un paquete aparte (payload), del
// que dependen collector, shipper y buffer sin acoplarse entre sí.
package payload

import "time"

// Payload es el cuerpo del heartbeat que se envía a POST {serverUrl}/ingest.
type Payload struct {
	AgentVersion string       `json:"agentVersion"`
	CollectedAt  time.Time    `json:"collectedAt"`
	Host         HostInfo     `json:"host"`
	Metrics      Metrics      `json:"metrics"`
	LocalAlerts  []LocalAlert `json:"localAlerts,omitempty"`
}

// HostInfo identifica la máquina y su contexto (README §9, bloque "host").
type HostInfo struct {
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Kernel    string `json:"kernel"`
	UptimeSec uint64 `json:"uptimeSec"`
}

// Metrics agrupa todas las familias de métricas. Los punteros/slices se
// omiten del JSON si el colector correspondiente no produjo datos.
type Metrics struct {
	CPU       *CPUMetrics      `json:"cpu,omitempty"`
	Memory    *MemoryMetrics   `json:"memory,omitempty"`
	Disk      []DiskMetrics    `json:"disk,omitempty"`
	Network   []NetworkMetrics `json:"network,omitempty"`
	Processes *ProcessMetrics  `json:"processes,omitempty"`
}

type CPUMetrics struct {
	UsagePct    float64   `json:"usagePct"`
	LoadAvg     []float64 `json:"loadAvg,omitempty"`
	CtxSwitches uint64    `json:"ctxSwitches"`
	PerCorePct  []float64 `json:"perCorePct"`
}

type MemoryMetrics struct {
	TotalBytes  uint64  `json:"totalBytes"`
	UsedBytes   uint64  `json:"usedBytes"`
	UsagePct    float64 `json:"usagePct"`
	SwapUsedPct float64 `json:"swapUsedPct"`
}

type DiskMetrics struct {
	Mount     string  `json:"mount"`
	UsagePct  float64 `json:"usagePct"`
	FreeBytes uint64  `json:"freeBytes"`
}

type NetworkMetrics struct {
	Iface         string  `json:"iface"`
	RxBytesPerSec float64 `json:"rxBytesPerSec"`
	TxBytesPerSec float64 `json:"txBytesPerSec"`
	ErrIn         uint64  `json:"errIn"`
	ErrOut        uint64  `json:"errOut"`
}

type ProcessMetrics struct {
	Total  uint64        `json:"total"`
	Zombie uint64        `json:"zombie"`
	TopCPU []ProcessInfo `json:"topCpu"`
	TopMem []ProcessInfo `json:"topMem"`
}

type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpuPct,omitempty"`
	MemPct float64 `json:"memPct,omitempty"`
}

// LocalAlert es opcional: la autoridad de detección es el backend (README §1).
type LocalAlert struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
