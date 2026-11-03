package host

import "github.com/godbus/dbus/v5"

// trayAvailable reports whether a StatusNotifierItem host is running. Stock GNOME has none.
func trayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var owned bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owned)
	return err == nil && owned
}
