//go:build linux

package focus

// New returns the focuser for GNOME Terminal tabs.
func New() Focuser { return NewGnomeTerminal() }
