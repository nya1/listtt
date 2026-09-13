//go:build darwin

package focus

// New returns the focuser for Terminal.app tabs.
func New() Focuser { return NewTerminalApp() }
