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
	// Inventory viaja en su propio ciclo (agent.inventoryLoop), distinto del
	// heartbeat: se adjunta una sola vez al primer payload tras cada escaneo
	// y se omite en el resto, para no cargar cada heartbeat con el listado
	// completo de software instalado.
	Inventory *Inventory `json:"inventory,omitempty"`
}

// HostInfo identifica la máquina y su contexto (README §9, bloque "host").
type HostInfo struct {
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Kernel    string `json:"kernel"`
	UptimeSec uint64 `json:"uptimeSec"`
	// VirtualizationSystem (kvm, vmware, hyperv, xen, docker...) y
	// VirtualizationRole ("guest" o "host") permiten al backend distinguir
	// "esta máquina no tiene sensores de potencia" de "esta máquina es un
	// invitado, y su consumo eléctrico lo mide el equipo físico que la
	// hospeda" (P29): un invitado no tiene registros de energía que leer, y
	// eso no es un defecto de su hardware. Ambos se omiten cuando gopsutil no
	// los detecta, en vez de enviar una cadena vacía.
	VirtualizationSystem string `json:"virtualizationSystem,omitempty"`
	VirtualizationRole   string `json:"virtualizationRole,omitempty"`
}

type Inventory struct {
	Software []Software `json:"software"`
}

type Software struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Vendor       string    `json:"vendor"`
	Version      string    `json:"version"`
	GUID         string    `json:"guid"`
	InstalledAt  time.Time `json:"installedAt"`
	InstallPath  string    `json:"installPath"`
	Architecture string    `json:"architecture"`
	SizeBytes    uint64    `json:"sizeBytes"`
	Status       string    `json:"status"`
	Source       string    `json:"source"`
}

// Metrics agrupa todas las familias de métricas. Los punteros/slices se
// omiten del JSON si el colector correspondiente no produjo datos.
type Metrics struct {
	CPU       *CPUMetrics      `json:"cpu,omitempty"`
	Memory    *MemoryMetrics   `json:"memory,omitempty"`
	Disk      []DiskMetrics    `json:"disk,omitempty"`
	Network   []NetworkMetrics `json:"network,omitempty"`
	Processes *ProcessMetrics  `json:"processes,omitempty"`
	Power     *PowerMetrics    `json:"power,omitempty"`
}

// PowerMetrics es el consumo eléctrico del host. Watts es potencia
// instantánea o su mejor aproximación; Estimated distingue una lectura de
// sensor de una construcción nuestra; Source dice de dónde salió, para que
// un número raro se pueda auditar sin abrir el agente.
//
// El puntero en Metrics.Power no es cosmético: permite distinguir "esta
// máquina no tiene ninguna fuente de potencia" (campo ausente) de "hay
// fuente y marca cero" (watts: 0), que es la distinción sobre la que se
// apoya todo el proyecto de consumo energético.
//
// Source es una cadena libre y no un enum a propósito: los valores previstos
// (rapl, hwmon, nvidia, amd_gpu, psu, model, y combinaciones como
// rapl+nvidia) crecerán según aparezcan proveedores nuevos, y encerrarlos en
// un tipo obligaría a versionar el contrato cada vez.
type PowerMetrics struct {
	Watts     float64 `json:"watts"`
	Estimated bool    `json:"estimated"`
	Source    string  `json:"source"`
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
