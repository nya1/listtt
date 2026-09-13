// Package focus raises the terminal tab that runs a given process.
package focus

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrUnsupported means there is no focus adapter for this platform.
var ErrUnsupported = errors.New("terminal focus is not supported on this platform")

const commandTimeout = 5 * time.Second

// Focuser has the same method set as dashboard.Focuser.
type Focuser interface {
	CanFocus(pid int) bool
	Focus(pid int) error
}

// Runner runs a command and returns stdout and stderr separately.
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// run executes a command with the focus timeout and returns trimmed output.
func run(r Runner, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	stdout, stderr, err := r(ctx, name, args...)
	return strings.TrimSpace(string(stdout)), strings.TrimSpace(string(stderr)), err
}
