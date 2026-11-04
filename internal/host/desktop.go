package host

import (
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	_ "embed"

	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

//go:embed tray-template.png
var trayTemplateIcon []byte

//go:embed tray-light.png
var trayLightIcon []byte

const (
	mainTitle   = "devt-tunnel"
	quickWidth  = 640
	quickHeight = 420
)

// desktop owns the windows, tray and global shortcut.
type desktop struct {
	app   *application.App
	svc   *Service
	main  *application.WebviewWindow
	quick *application.WebviewWindow
	tray  *application.SystemTray

	mu      sync.Mutex
	traySig string
	hasTray bool
}

// Attach creates the desktop integration and starts tunnels marked to start on launch.
func Attach(app *application.App, svc *Service) {
	d := &desktop{app: app, svc: svc, hasTray: trayAvailable()}
	d.createWindows()
	if d.hasTray {
		d.createTray()
	}
	svc.mu.Lock()
	svc.host = d
	svc.mu.Unlock()
	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) { svc.manager.Wake() })
	go monitorClockJump(svc.manager.Wake)

	// Registered after startup so that a conflict comes back here rather than to the global error handler.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		log.Printf("application ready")
		d.registerShortcut()
		d.refreshTray(svc.views())
		for _, t := range svc.manager.Config().Tunnels {
			if t.StartOnLaunch {
				if err := svc.manager.Start(t.ID); err != nil {
					log.Printf("start %s: %v", t.ID, err)
				}
			}
		}
	})
}

func (d *desktop) createWindows() {
	d.main = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            mainTitle,
		Width:            1180,
		Height:           760,
		MinWidth:         760,
		MinHeight:        480,
		URL:              "/",
		BackgroundColour: application.NewRGB(243, 243, 241),
	})
	// Without a tray, a hidden window could not be reopened and tunnels would run invisibly.
	d.main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		if d.hasTray {
			d.main.Hide()
			return
		}
		d.app.Quit()
	})
	d.main.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		d.svc.secrets.Forget()
		if changed, err := d.svc.configStore.Changed(); err == nil && changed {
			d.svc.loadConfig()
		}
	})

	d.quick = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "quick",
		Title:            "Quick launch",
		Width:            quickWidth,
		Height:           quickHeight,
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		URL:              "/?view=quick",
		BackgroundColour: application.NewRGB(255, 255, 255),
	})
	d.quick.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		d.quick.Hide()
	})
	d.quick.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		d.quick.Hide()
	})
}

func monitorClockJump(wake func()) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	previous := time.Now().Round(0)
	for now := range ticker.C {
		wall := now.Round(0)
		if clockJumped(previous, wall, 30*time.Second) {
			wake()
		}
		previous = wall
	}
}

func clockJumped(previous, current time.Time, interval time.Duration) bool {
	return current.Sub(previous) > interval+60*time.Second
}

func (d *desktop) showMain() {
	d.main.Show()
	d.main.Focus()
}

func (d *desktop) toggleQuick() {
	if d.quick.IsVisible() {
		d.quick.Hide()
		return
	}
	d.quick.Center()
	d.quick.Show()
	d.quick.Focus()
	d.app.Event.Emit("quick:opened")
}

func defaultShortcut() string {
	// Alt+Space opens the window menu on Windows and on many Linux desktops.
	if runtime.GOOS == "darwin" {
		return "Alt+Space"
	}
	return "Ctrl+Alt+Space"
}

func (d *desktop) registerShortcut() {
	accel := d.svc.settings.Shortcut
	if accel == "" {
		accel = defaultShortcut()
	}
	err := d.setShortcut(accel)
	if err != nil {
		log.Printf("global shortcut: %v", err)
		d.svc.notify()
	}
}

func (d *desktop) setShortcut(accel string) error {
	if accel == "" {
		accel = defaultShortcut()
	}
	d.svc.mu.Lock()
	old := d.svc.shortcut
	d.svc.mu.Unlock()
	if old == accel && d.app.GlobalShortcut.IsRegistered(accel) {
		return nil
	}
	err := d.app.GlobalShortcut.Register(accel, d.toggleQuick)
	if err != nil {
		return err
	}
	if old != "" && old != accel {
		_ = d.app.GlobalShortcut.Unregister(old)
	}
	d.svc.mu.Lock()
	d.svc.shortcut = accel
	d.svc.shortcutErr = ""
	d.svc.mu.Unlock()
	return nil
}

func (d *desktop) createTray() {
	d.tray = d.app.SystemTray.New()
	if runtime.GOOS == "darwin" {
		d.tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		d.tray.SetIcon(trayLightIcon)
	}
	d.tray.SetTooltip(mainTitle)
}

// refreshTray rebuilds the menu only when its content changed, so that an open menu is not disturbed.
func (d *desktop) refreshTray(views []TunnelView) {
	if d.tray == nil {
		return
	}
	listed := trayTunnels(views)
	var sig strings.Builder
	for _, v := range listed {
		sig.WriteString(v.Tunnel.ID + "\x00" + v.Tunnel.Name + "\x00" + string(v.Status.State) + "\n")
	}
	d.mu.Lock()
	unchanged := d.traySig == sig.String() && d.traySig != ""
	d.traySig = sig.String()
	d.mu.Unlock()
	if unchanged {
		return
	}

	menu := d.app.NewMenu()
	for _, v := range listed {
		id := v.Tunnel.ID
		label := v.Tunnel.Name
		if v.Status.State != supervisor.StateStopped && v.Status.State != supervisor.StateRunning {
			label += " (" + stateLabel(v.Status.State) + ")"
		}
		menu.AddCheckbox(label, isActive(v.Status.State)).OnClick(func(*application.Context) {
			if err := d.svc.Toggle(id); err != nil {
				log.Printf("toggle %s: %v", id, err)
			}
		})
	}
	if len(listed) > 0 {
		menu.AddSeparator()
	}
	menu.Add("Quick launch").OnClick(func(*application.Context) { d.toggleQuick() })
	menu.Add("Open devt-tunnel").OnClick(func(*application.Context) { d.showMain() })
	menu.AddSeparator()
	menu.Add("Stop all and quit").OnClick(func(*application.Context) { d.app.Quit() })
	d.tray.SetMenu(menu)
}

// trayTunnels lists favorites and anything currently active.
func trayTunnels(views []TunnelView) []TunnelView {
	var out []TunnelView
	for _, v := range views {
		if v.Tunnel.Favorite || isActive(v.Status.State) {
			out = append(out, v)
		}
	}
	return out
}

func isActive(s supervisor.State) bool {
	switch s {
	case supervisor.StateStarting, supervisor.StateRunning, supervisor.StateReconnecting:
		return true
	}
	return false
}

func stateLabel(s supervisor.State) string {
	switch s {
	case supervisor.StateStarting:
		return "starting"
	case supervisor.StateReconnecting:
		return "reconnecting"
	case supervisor.StateFailed:
		return "failed"
	case supervisor.StateNeedsToken:
		return "needs token"
	case supervisor.StateKeyringUnavailable:
		return "keyring unavailable"
	}
	return string(s)
}
