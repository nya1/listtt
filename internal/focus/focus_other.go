//go:build !linux && !darwin

package focus

// New returns a focuser that never focuses anything.
func New() Focuser { return unsupported{} }

type unsupported struct{}

func (unsupported) CanFocus(int) bool { return false }

func (unsupported) Focus(int) error { return ErrUnsupported }
