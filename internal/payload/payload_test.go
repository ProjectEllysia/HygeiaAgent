package payload

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPayloadJSON_RoundTrip(t *testing.T) {
	p := &Payload{
		AgentVersion: "1.0.0",
		CollectedAt:  mustParseTime("2025-01-01T00:00:00Z"),
		Host: HostInfo{
			Hostname:  "server-01",
			OS:        "linux",
			Kernel:    "6.1.0",
			UptimeSec: 3600,
		},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}

	var got Payload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}

	if got.AgentVersion != p.AgentVersion {
		t.Errorf("AgentVersion = %q, want %q", got.AgentVersion, p.AgentVersion)
	}
	if got.Host.Hostname != p.Host.Hostname {
		t.Errorf("Host.Hostname = %q, want %q", got.Host.Hostname, p.Host.Hostname)
	}
	if got.Host.OS != p.Host.OS {
		t.Errorf("Host.OS = %q, want %q", got.Host.OS, p.Host.OS)
	}
	if got.Host.UptimeSec != p.Host.UptimeSec {
		t.Errorf("Host.UptimeSec = %d, want %d", got.Host.UptimeSec, p.Host.UptimeSec)
	}
}

func TestPayloadJSON_OmitsEmptyMetrics(t *testing.T) {
	p := &Payload{AgentVersion: "1.0.0"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if !strings.Contains(string(data), "metrics") {
		t.Errorf("expected metrics to be present (non-pointer struct), got %s", data)
	}
}

func TestPayloadJSON_HostInfo(t *testing.T) {
	h := HostInfo{
		Hostname: "test-host",
		OS:       "darwin",
		Kernel:   "23.0.0",
	}
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}

	var got HostInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if got.Hostname != h.Hostname {
		t.Errorf("Hostname = %q", got.Hostname)
	}
}

func TestMetricsJSON_CPUMetrics(t *testing.T) {
	m := Metrics{
		CPU: &CPUMetrics{
			UsagePct:    42.5,
			LoadAvg:     []float64{1.0, 0.8, 0.5},
			CtxSwitches: 12345,
			PerCorePct:  []float64{50.0, 35.0},
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}

	var got Metrics
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if got.CPU.UsagePct != 42.5 {
		t.Errorf("CPU.UsagePct = %f", got.CPU.UsagePct)
	}
	if len(got.CPU.LoadAvg) != 3 {
		t.Errorf("len(CPU.LoadAvg) = %d", len(got.CPU.LoadAvg))
	}
}

func TestMetricsJSON_EmptySlices(t *testing.T) {
	m := Metrics{
		Disk:    []DiskMetrics{},
		Network: []NetworkMetrics{},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if strings.Contains(string(data), `"disk"`) {
		t.Errorf("expected empty disk to be omitted, got %s", data)
	}
}

func TestProcessInfoJSON(t *testing.T) {
	p := ProcessInfo{
		PID:    1234,
		Name:   "bash",
		CPUPct: 5.0,
		MemPct: 1.5,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}

	var got ProcessInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if got.PID != 1234 {
		t.Errorf("PID = %d", got.PID)
	}
}

func TestMetricsJSON_NilPowerOmitsKey(t *testing.T) {
	m := Metrics{CPU: &CPUMetrics{UsagePct: 10}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if strings.Contains(string(data), `"power"`) {
		t.Errorf("esperaba que power estuviera ausente sin fuente de potencia, got %s", data)
	}
}

func TestMetricsJSON_ZeroWattsPowerIsPresent(t *testing.T) {
	m := Metrics{Power: &PowerMetrics{Watts: 0, Estimated: false, Source: "rapl"}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if !strings.Contains(string(data), `"power":{"watts":0,"estimated":false,"source":"rapl"}`) {
		t.Errorf("esperaba power con watts:0 presente (distinto de ausente), got %s", data)
	}

	var got Metrics
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if got.Power == nil {
		t.Fatal("Power = nil tras round-trip, se esperaba un valor con watts:0")
	}
	if got.Power.Watts != 0 || got.Power.Source != "rapl" {
		t.Errorf("Power = %+v", got.Power)
	}
}

func TestLocalAlertJSON(t *testing.T) {
	a := LocalAlert{
		Type:    "high_cpu",
		Message: "CPU usage above 90%",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}

	var got LocalAlert
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if got.Type != "high_cpu" {
		t.Errorf("Type = %q", got.Type)
	}
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
