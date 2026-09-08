package collector

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/payload"
)

func newPowerProvider(log *slog.Logger) PowerProvider {
	return &linuxPowerProvider{
		hwmon:  newHwmonProvider(log),
		rapl:   newRAPLProvider(log),
		nvidia: newNVIDIAGPUProvider(log),
		amd:    newAMDGPUProvider(log),
	}
}

// linuxPowerProvider orquesta las cuatro fuentes de Linux aplicando la
// política de agregación de P12, evaluada en este orden:
//
//  1. Si hay una fuente de SISTEMA COMPLETO (hwmon reconocido: una fuente de
//     alimentación o un controlador de gestión), se usa SOLA — ya lo incluye
//     todo, y sumarle cualquier otra cosa contaría dos veces.
//  2. Si no, se suman las fuentes por COMPONENTE que no se solapan entre sí:
//     RAPL (package de CPU) + GPU NVIDIA + GPU AMD.
//  3. Si no hay ninguna, nil: no hay potencia que reportar en esta máquina.
//
// El campo Source no es decorativo: enumera exactamente los sumandos, para
// que una cifra sospechosa se explique leyendo el payload en vez de
// reproduciendo el hardware de la máquina.
type linuxPowerProvider struct {
	hwmon  PowerProvider
	rapl   PowerProvider
	nvidia PowerProvider
	amd    PowerProvider
}

func (p *linuxPowerProvider) Read(ctx context.Context) (*payload.PowerMetrics, error) {
	if hw, _ := p.hwmon.Read(ctx); hw != nil {
		return hw, nil
	}

	var watts float64
	var sources []string
	if r, _ := p.rapl.Read(ctx); r != nil {
		watts += r.Watts
		sources = append(sources, r.Source)
	}
	if g, _ := p.nvidia.Read(ctx); g != nil {
		watts += g.Watts
		sources = append(sources, g.Source)
	}
	if a, _ := p.amd.Read(ctx); a != nil {
		watts += a.Watts
		sources = append(sources, a.Source)
	}
	if len(sources) == 0 {
		return nil, nil
	}
	return &payload.PowerMetrics{Watts: watts, Estimated: true, Source: strings.Join(sources, "+")}, nil
}

// diagnosePower implementa DiagnosePower (declarado en power.go) para Linux,
// pensado para `doctor` (P08). No usa las instancias con estado del propio
// agente: una sola lectura de energy_uj ya distingue "legible" de "sin
// privilegio" sin necesitar dos ciclos de heartbeat para derivar un vatio.
func diagnosePower() PowerDiagnostic {
	if d := diagnoseRAPL(); d != nil {
		return *d
	}

	silent := discardSlog()
	if hw, _ := newHwmonProvider(silent).Read(context.Background()); hw != nil {
		return PowerDiagnostic{Available: true, Detail: "sensor de " + hw.Source}
	}
	if gpu, _ := newNVIDIAGPUProvider(silent).Read(context.Background()); gpu != nil {
		return PowerDiagnostic{Available: true, Detail: "GPU NVIDIA vía nvidia-smi"}
	}
	if gpu, _ := newAMDGPUProvider(silent).Read(context.Background()); gpu != nil {
		return PowerDiagnostic{Available: true, Detail: "GPU AMD vía hwmon"}
	}
	return PowerDiagnostic{
		Available: false,
		Detail:    "ninguna fuente de potencia detectada (RAPL, hwmon reconocido ni GPU)",
	}
}

// diagnoseRAPL devuelve un diagnóstico solo cuando RAPL tiene algo que decir
// (presente y legible, o presente sin privilegio); nil deja que el llamador
// siga mirando las demás fuentes, porque la ausencia de RAPL no es un
// diagnóstico en sí misma — hwmon o una GPU podrían seguir dando un dato.
func diagnoseRAPL() *PowerDiagnostic {
	domains := discoverRAPLDomains(defaultRAPLBasePath)
	if len(domains) == 0 {
		return nil
	}

	readable, permissionDenied := 0, false
	for _, d := range domains {
		raw, err := os.ReadFile(filepath.Join(d, "energy_uj"))
		switch {
		case err == nil:
			if _, perr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); perr == nil {
				readable++
			}
		case os.IsPermission(err):
			permissionDenied = true
		}
	}

	if readable > 0 {
		return &PowerDiagnostic{Available: true, Detail: "RAPL legible (" + strconv.Itoa(readable) + "/" + strconv.Itoa(len(domains)) + " dominio(s))"}
	}
	if permissionDenied {
		return &PowerDiagnostic{
			Available: false,
			Detail:    "RAPL presente pero sin privilegio de lectura",
			Hint:      "energy_uj es 0400 desde Linux 5.10 (CVE-2020-8694, PLATYPUS). Ejecuta como root, o instala el agente como servicio.",
		}
	}
	return &PowerDiagnostic{Available: false, Detail: "RAPL presente pero ilegible"}
}

// discardSlog es un logger que descarta todo: los proveedores usados solo
// para diagnóstico puntual no deben dejar avisos "una sola vez" consumidos
// que luego el collector real ya no podría emitir.
func discardSlog() *slog.Logger { return slog.New(slog.DiscardHandler) }
