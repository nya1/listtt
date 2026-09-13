package focus

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const gnomeScreenPrefix = "GNOME_TERMINAL_SCREEN=/org/gnome/Terminal/screen/"

var gnomeScreenID = regexp.MustCompile(`^[0-9a-f]{8}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{12}$`)

// GnomeTerminal focuses GNOME Terminal tabs through the terminal's
// org.gnome.Shell.SearchProvider2 D-Bus interface.
type GnomeTerminal struct {
	ProcRoot string // "/proc" outside tests
	Run      Runner
}

func NewGnomeTerminal() GnomeTerminal {
	return GnomeTerminal{ProcRoot: "/proc", Run: execRunner}
}

// CanFocus requires a gnome-terminal-server ancestor (so tmux or VS Code
// terminals with an inherited GNOME_TERMINAL_SCREEN are excluded) and a
// GNOME_TERMINAL_SCREEN value in the process environment.
func (g GnomeTerminal) CanFocus(pid int) bool {
	if !g.hasGnomeTerminalAncestor(pid) {
		debugf("CanFocus pid=%d: no gnome-terminal ancestor", pid)
		return false
	}
	uuid, err := g.screenUUID(pid)
	if err != nil {
		debugf("CanFocus pid=%d: screenUUID error: %v", pid, err)
		return false
	}
	debugf("CanFocus pid=%d: screenUUID=%s ok", pid, uuid)
	return true
}

func (g GnomeTerminal) Focus(pid int) error {
	uuid, err := g.screenUUID(pid)
	if err != nil {
		debugf("Focus pid=%d: screenUUID error: %v", pid, err)
		return err
	}
	debugf("Focus pid=%d: screenUUID=%s", pid, uuid)
	_, stderr, err := run(g.Run, "gdbus", "call", "--session",
		"--dest", "org.gnome.Terminal",
		"--object-path", "/org/gnome/Terminal/SearchProvider",
		"--method", "org.gnome.Shell.SearchProvider2.ActivateResult",
		"'"+uuid+"'", "[]", "0")
	debugf("Focus pid=%d: gdbus err=%v stderr=%q", pid, err, stderr)
	if err != nil {
		return fmt.Errorf("GNOME Terminal focus failed (%v): %s", err, stderr)
	}
	return nil
}

// screenUUID reads GNOME_TERMINAL_SCREEN and returns the tab UUID with dashes.
func (g GnomeTerminal) screenUUID(pid int) (string, error) {
	env, err := os.ReadFile(filepath.Join(g.ProcRoot, strconv.Itoa(pid), "environ"))
	if err != nil {
		return "", fmt.Errorf("read environment of pid %d: %w", pid, err)
	}
	for _, kv := range bytes.Split(env, []byte{0}) {
		id, ok := strings.CutPrefix(string(kv), gnomeScreenPrefix)
		if !ok {
			continue
		}
		if !gnomeScreenID.MatchString(id) {
			return "", fmt.Errorf("unexpected GNOME_TERMINAL_SCREEN tab id %q", id)
		}
		return strings.ReplaceAll(id, "_", "-"), nil
	}
	return "", fmt.Errorf("pid %d has no GNOME_TERMINAL_SCREEN", pid)
}

// hasGnomeTerminalAncestor walks parent processes. /proc/<pid>/comm is cut to
// 15 characters, so gnome-terminal-server appears as "gnome-terminal-".
func (g GnomeTerminal) hasGnomeTerminalAncestor(pid int) bool {
	for hops := 0; hops < 64; hops++ {
		ppid, err := g.parentPID(pid)
		if err != nil || ppid <= 1 {
			return false
		}
		comm, err := os.ReadFile(filepath.Join(g.ProcRoot, strconv.Itoa(ppid), "comm"))
		if err != nil {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(string(comm)), "gnome-terminal-") {
			return true
		}
		pid = ppid
	}
	return false
}

// parentPID reads field 4 of /proc/<pid>/stat: "pid (comm) state ppid ...".
// comm can contain spaces and parentheses, so parse after the last ')'.
func (g GnomeTerminal) parentPID(pid int) (int, error) {
	stat, err := os.ReadFile(filepath.Join(g.ProcRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	i := bytes.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}
	return strconv.Atoi(fields[1])
}
