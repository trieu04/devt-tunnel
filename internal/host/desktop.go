package host

import (
	"log"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	_ "embed"
)

//go:embed tray-template.png
var trayTemplateIcon []byte

//go:embed tray-light.png
var trayLightIcon []byte

const (
	mainTitle   = "devt-tunnel"
	quickWidth  = 640
	quickHeight = 420
	trayWidth   = 360
	trayHeight  = 540
)

// desktop owns the windows, tray and global shortcut.
type desktop struct {
	app   *application.App
	svc   *Service
	main  *application.WebviewWindow
	quick *application.WebviewWindow
	popup *application.WebviewWindow
	tray  *application.SystemTray

	hasTray bool
}

// Attach creates the desktop integration and requests marked tunnels after application startup.
func Attach(app *application.App, svc *Service) {
	d := &desktop{app: app, svc: svc, hasTray: trayAvailable()}
	d.createWindows()
	d.createMenu()
	if d.hasTray {
		d.createTray()
	}
	svc.mu.Lock()
	svc.host = d
	svc.mu.Unlock()
	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) { svc.wake() })

	// Registered after startup so that a conflict comes back here rather than to the global error handler.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		log.Printf("application ready")
		svc.applicationReady()
		d.registerShortcut()
		svc.startMarked()
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
		d.svc.focus()
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

	if d.hasTray {
		d.popup = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:             "tray",
			Title:            mainTitle,
			Width:            trayWidth,
			Height:           trayHeight,
			MinWidth:         trayWidth,
			MinHeight:        trayHeight,
			MaxWidth:         trayWidth,
			MaxHeight:        trayHeight,
			Hidden:           true,
			Frameless:        true,
			AlwaysOnTop:      true,
			DisableResize:    true,
			HideOnFocusLost:  true,
			HideOnEscape:     true,
			URL:              "/?view=tray",
			BackgroundColour: application.NewRGB(255, 255, 255),
			Mac: application.MacWindow{
				CornerType: application.MacWindowCornerTypeSquare,
			},
			Windows: application.WindowsWindow{
				HiddenOnTaskbar: true,
			},
		})
		d.popup.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			d.popup.Hide()
		})
	}
}

func (d *desktop) showMain() {
	if d.popup != nil {
		d.popup.Hide()
	}
	d.main.Show()
	d.main.Focus()
}

func (d *desktop) showQuick() {
	if d.popup != nil {
		d.popup.Hide()
	}
	d.quick.Center()
	d.quick.Show()
	d.quick.Focus()
	d.app.Event.Emit("quick:opened")
}

func (d *desktop) toggleQuick() {
	if d.quick.IsVisible() {
		d.quick.Hide()
		return
	}
	d.showQuick()
}

func defaultShortcut() string {
	// Alt+Space opens the window menu on Windows and on many Linux desktops.
	if runtime.GOOS == "darwin" {
		return "Alt+Space"
	}
	return "Ctrl+Alt+Space"
}

func (d *desktop) registerShortcut() {
	accel := d.svc.shortcutSetting()
	if accel == "" {
		accel = defaultShortcut()
	}
	err := d.setShortcut(accel)
	if err != nil {
		log.Printf("global shortcut: %v", err)
		d.app.Event.Emit(eventTunnels)
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
	d.tray.AttachWindow(d.popup).WindowOffset(4)
}

// createMenu installs the macOS menu bar with an app-menu "Settings…" entry (Cmd+,).
func (d *desktop) createMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	menu := d.app.Menu.New()

	appMenu := menu.AddSubmenu(mainTitle)
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	appMenu.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
		d.svc.OpenIn("settings", "")
	})
	appMenu.AddSeparator()
	appMenu.AddRole(application.ServicesMenu)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.UnHide)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	d.app.Menu.Set(menu)
}
