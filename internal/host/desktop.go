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
	if runtime.GOOS == "linux" {
		// Wails positions the popup at the click point, which StatusNotifier hosts often
		// report as (0,0), leaving it centred. Anchor it to the panel edge instead.
		d.tray.OnClick(d.toggleLinuxPopup)
		return
	}
	d.tray.AttachWindow(d.popup).WindowOffset(4)
}

// toggleLinuxPopup shows the tray popup in the screen corner next to the panel.
func (d *desktop) toggleLinuxPopup() {
	if d.popup.IsVisible() {
		d.popup.Hide()
		return
	}
	if screen := d.app.Screen.GetPrimary(); screen != nil {
		x, y := trayPopupPosition(screen.Bounds, screen.WorkArea, trayWidth, trayHeight, 4)
		d.popup.SetPosition(x, y)
	}
	d.popup.Show().Focus()
}

// trayPopupPosition finds the panel edge from the difference between the screen bounds
// and the work area, then places the popup against it, on the right (where trays live).
func trayPopupPosition(bounds, work application.Rect, w, h, gap int) (int, int) {
	x := work.X + work.Width - w - gap
	y := work.Y + gap
	switch {
	case work.Y+work.Height < bounds.Y+bounds.Height: // panel at the bottom
		y = work.Y + work.Height - h - gap
	case work.X > bounds.X && work.X+work.Width >= bounds.X+bounds.Width: // panel on the left
		x = work.X + gap
		y = work.Y + work.Height - h - gap
	case work.X+work.Width < bounds.X+bounds.Width: // panel on the right
		x = work.X + work.Width - w - gap
		y = work.Y + work.Height - h - gap
	}
	if x < work.X {
		x = work.X
	}
	if y < work.Y {
		y = work.Y
	}
	return x, y
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
