// Command hygeia-tray es el companion de escritorio (§11): un icono en la
// bandeja del sistema que refleja el estado del servicio y permite dar de
// alta el agente sin editar el TOML a mano.
//
// NO recolecta ni envía métricas (§11.6) — solo habla con el servicio por el
// canal de control local. Un servidor headless corre hygeia-agent solo, sin
// este binario, exactamente igual.
//
// En Windows compílalo sin consola:
//
//	go build -ldflags "-H=windowsgui" ./cmd/hygeia-tray
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"fyne.io/systray"
	"github.com/ncruces/zenity"

	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/autostart"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/control"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/icon"
	"github.com/ProjectEllysia/Ellysia-Hygeia/internal/version"
)

// pollInterval es cada cuánto el tray pregunta el estado al servicio. Es
// solo refresco de UI: no tiene nada que ver con el intervalo de heartbeat
// del agente, que lo marca el backend (§1).
const pollInterval = 5 * time.Second

// tickInterval refresca solo el texto de "Último envío" (relativo: "hace Ns")
// entre sondeos reales al servicio, para que el contador no se quede parado
// hasta el siguiente pollInterval.
const tickInterval = 1 * time.Second

// menu agrupa los elementos cuyo texto se refresca en cada sondeo.
type menu struct {
	state      *systray.MenuItem
	lastPush   *systray.MenuItem
	buffer     *systray.MenuItem
	host       *systray.MenuItem
	enroll     *systray.MenuItem
	reset      *systray.MenuItem
	lastPushAt time.Time // último valor conocido, para el retick de 1s sin volver a sondear
}

func main() {
	// Subcomandos de arranque automático. Se ejecutan y salen sin abrir la
	// bandeja: son para el instalador, no para el uso diario.
	if len(os.Args) > 1 {
		if err := runCommand(os.Args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "hygeia-tray: %v\n", err)
			os.Exit(1)
		}
		return
	}
	systray.Run(onReady, func() {})
}

func runCommand(cmd string) error {
	switch cmd {
	case "enable-autostart":
		if err := autostart.Enable(); err != nil {
			return err
		}
		fmt.Println("hygeia-tray: arrancará con la sesión de este usuario")
		return nil

	case "disable-autostart":
		if err := autostart.Disable(); err != nil {
			return err
		}
		fmt.Println("hygeia-tray: ya no arrancará con la sesión")
		return nil

	case "status":
		on, err := autostart.Enabled()
		if err != nil {
			return err
		}
		fmt.Printf("hygeia-tray: arranque automático %s\n", map[bool]string{true: "activado", false: "desactivado"}[on])
		return nil

	case "version", "-v", "--version":
		fmt.Printf("hygeia-tray %s\n", version.Version)
		return nil

	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil

	default:
		return fmt.Errorf("subcomando desconocido %q\n\n%s", cmd, usage)
	}
}

const usage = `Uso: hygeia-tray [subcomando]

  (sin argumentos)    abre el icono en la bandeja del sistema
  enable-autostart    arranca con la sesión de este usuario
  disable-autostart   deja de arrancar con la sesión
  status              consulta si el arranque automático está activo
  version
`

func onReady() {
	systray.SetIcon(icon.Unconfigured())
	systray.SetTitle("Hygeia")
	systray.SetTooltip("Hygeia — conectando con el servicio…")

	m := &menu{
		state:    systray.AddMenuItem("Estado: consultando…", "Estado del agente local"),
		lastPush: systray.AddMenuItem("Último envío: —", "Momento del último heartbeat aceptado"),
		buffer:   systray.AddMenuItem("Buffer: —", "Payloads pendientes de enviar"),
		host:     systray.AddMenuItem("Host: —", "Activo monitorizado"),
	}
	// Los items informativos no son accionables: deshabilitarlos evita que
	// parezcan botones.
	m.state.Disable()
	m.lastPush.Disable()
	m.buffer.Disable()
	m.host.Disable()

	systray.AddSeparator()
	m.enroll = systray.AddMenuItem("Introducir clave de agente…", "Dar de alta este activo")
	m.enroll.Hide()
	m.reset = systray.AddMenuItem("Reestablecer configuración…", "Borra la clave guardada para volver a dar de alta el activo")
	m.reset.Hide()

	startup := systray.AddMenuItemCheckbox("Arrancar con la sesión", "Abrir este icono al iniciar sesión", autostartEnabled())
	systray.AddMenuItem(fmt.Sprintf("Versión %s", version.Version), "").Disable()
	// "Ocultar icono", no "Salir": esto cierra SOLO el companion de bandeja.
	// El servicio (hygeia-agent) sigue recolectando y enviando — no puede
	// pararlo desde aquí (§11.7: el canal de control no expone start/stop) ni
	// debería: es un proceso sin privilegios y el activo dejaría de reportar
	// sin que nadie se entere. Llamarlo "Salir" prometía lo contrario.
	quit := systray.AddMenuItem("Ocultar icono", "Cierra el icono de la bandeja; el servicio sigue monitorizando este activo")

	client := control.NewClient()
	refresh(client, m)

	ticker := time.NewTicker(pollInterval)
	uiTicker := time.NewTicker(tickInterval)
	go func() {
		defer ticker.Stop()
		defer uiTicker.Stop()
		for {
			select {
			case <-ticker.C:
				refresh(client, m)
			case <-uiTicker.C:
				if !m.lastPushAt.IsZero() {
					m.lastPush.SetTitle("Último envío: " + formatLastPush(m.lastPushAt))
				}
			case <-m.enroll.ClickedCh:
				promptEnroll(client, m)
			case <-m.reset.ClickedCh:
				promptReset(client, m)
			case <-startup.ClickedCh:
				toggleAutostart(startup)
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func autostartEnabled() bool {
	on, err := autostart.Enabled()
	if err != nil {
		log.Printf("tray: consultando arranque automático: %v", err)
		return false
	}
	return on
}

// toggleAutostart invierte el registro de arranque. El check del menú solo
// se mueve si la operación tuvo éxito: mostrar un estado que no se persistió
// haría creer al usuario que el tray volverá solo tras reiniciar.
func toggleAutostart(item *systray.MenuItem) {
	var err error
	if item.Checked() {
		err = autostart.Disable()
	} else {
		err = autostart.Enable()
	}
	if err != nil {
		log.Printf("tray: cambiando arranque automático: %v", err)
		_ = zenity.Error(
			"No se pudo cambiar el arranque automático:\n\n"+err.Error(),
			zenity.Title("Hygeia"),
			zenity.ErrorIcon,
		)
		return
	}
	if item.Checked() {
		item.Uncheck()
	} else {
		item.Check()
	}
}

// refresh sondea el servicio y actualiza icono, tooltip y menú.
func refresh(client *control.Client, m *menu) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	st, err := client.Status(ctx)
	if err != nil {
		// El servicio no responde: no es lo mismo que "error local del
		// agente", pero desde la bandeja se ve igual de roto, así que se
		// muestra en rojo y se dice explícitamente qué pasa.
		systray.SetIcon(icon.LocalError())
		systray.SetTooltip("Hygeia — servicio no disponible")
		m.state.SetTitle("Estado: servicio no disponible")
		m.lastPush.SetTitle("Último envío: —")
		m.lastPushAt = time.Time{}
		m.buffer.SetTitle("Buffer: —")
		m.host.SetTitle("Host: —")
		m.enroll.Hide()
		m.reset.Hide()
		return
	}

	systray.SetIcon(icon.ForState(st.State))
	systray.SetTooltip("Hygeia — " + stateLabel(st.State))
	m.state.SetTitle("Estado: " + stateLabel(st.State))
	m.lastPush.SetTitle("Último envío: " + formatLastPush(st.LastPushAt))
	m.lastPushAt = st.LastPushAt
	m.buffer.SetTitle(fmt.Sprintf("Buffer: %d pendientes", st.BufferSize))

	host := st.Hostname
	if host == "" {
		host = "—"
	}
	m.host.SetTitle("Host: " + host)

	// La opción de enrollment solo aparece cuando hace falta: el servicio
	// rechaza reconfigurar un agente ya dado de alta (§11.7), así que
	// ofrecerla siempre sería ofrecer algo que va a fallar. "Reestablecer"
	// es su complemento exacto: solo tiene sentido cuando SÍ hay algo que
	// borrar (p. ej. una clave inválida que deja el agente en rojo sin ni
	// siquiera ofrecer el enrollment, porque cree que ya está configurado).
	switch st.State {
	case control.StateUnconfigured:
		m.enroll.Show()
		m.reset.Hide()
	case control.StateKeyRejected:
		// Desde F-03 se puede pegar la clave nueva directamente, sin pasar
		// antes por "Reestablecer": el servicio acepta el enrollment cuando
		// él mismo ha marcado la clave como rechazada. Se deja también el
		// reset a mano, que sigue siendo una salida válida.
		m.enroll.Show()
		m.reset.Show()
	default:
		m.enroll.Hide()
		m.reset.Show()
	}
}

// promptEnroll pide la clave y se la pasa al servicio (§11.3).
func promptEnroll(client *control.Client, m *menu) {
	key, err := zenity.Entry(
		"Pega la clave de agente que te mostró Ellysia al dar de alta el activo.\n"+
			"Formato: keyId.secreto",
		zenity.Title("Hygeia — dar de alta este activo"),
		zenity.EntryText(""),
		zenity.HideText(),
	)
	if err != nil {
		// Cancelar el diálogo es un flujo normal, no un error.
		if err != zenity.ErrCanceled {
			log.Printf("tray: diálogo de enrollment: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Enroll(ctx, key); err != nil {
		// El mensaje de error NUNCA incluye la clave (§5).
		_ = zenity.Error(
			"No se pudo dar de alta el agente:\n\n"+err.Error(),
			zenity.Title("Hygeia"),
			zenity.ErrorIcon,
		)
		return
	}

	_ = zenity.Info(
		"Activo dado de alta. El agente empezará a enviar métricas en el próximo ciclo.",
		zenity.Title("Hygeia"),
		zenity.InfoIcon,
	)
	refresh(client, m)
}

// promptReset pide confirmación antes de borrar la clave guardada (§11.7):
// es el rescate para el caso que motivó esta función — una clave inválida
// (revocada, mal copiada) deja al agente en rojo sin ofrecer "Introducir
// clave" porque, desde su punto de vista, ya está configurado. Solo borra
// la clave LOCAL; no revoca nada en el backend.
func promptReset(client *control.Client, m *menu) {
	err := zenity.Question(
		"Esto borra la clave de agente guardada y detiene el envío de\n"+
			"métricas hasta que introduzcas una clave nueva. ¿Continuar?",
		zenity.Title("Hygeia — reestablecer configuración"),
		zenity.QuestionIcon,
	)
	if err != nil {
		// Cancelar el diálogo es un flujo normal, no un error.
		if err != zenity.ErrCanceled {
			log.Printf("tray: diálogo de reset: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Reset(ctx); err != nil {
		_ = zenity.Error(
			"No se pudo reestablecer la configuración:\n\n"+err.Error(),
			zenity.Title("Hygeia"),
			zenity.ErrorIcon,
		)
		return
	}

	_ = zenity.Info(
		"Configuración reestablecida. Usa \"Introducir clave de agente…\" para volver a darlo de alta.",
		zenity.Title("Hygeia"),
		zenity.InfoIcon,
	)
	refresh(client, m)
}

func stateLabel(state string) string {
	switch state {
	case control.StateConnected:
		return "conectado"
	case control.StateLocalError:
		return "error local"
	case control.StateUnconfigured:
		return "sin configurar"
	case control.StateStarting:
		return "iniciando…"
	case control.StateKeyRejected:
		// Dice qué pasa y qué hacer. Antes esto se veía como "error local"
		// con el texto "backend devolvió status 401", que no es ninguna de
		// las dos cosas.
		return "clave rechazada — pide una nueva en Ellysia"
	default:
		return "desconocido"
	}
}

func formatLastPush(t time.Time) string {
	if t.IsZero() {
		return "nunca"
	}
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		// Reloj del agente por delante del nuestro: no vale la pena razonar
		// sobre ello, solo no mostrar un "hace -3s".
		return t.Local().Format("15:04:05")
	}
	return fmt.Sprintf("hace %s", d)
}
