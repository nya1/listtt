package focus

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var macTTY = regexp.MustCompile(`^ttys[0-9]+$`)

// focusScript is the spec §7.4 AppleScript. It is passed to osascript as one
// -e argument per line. The tty is passed only through argv.
// Window activation uses `set frontmost to true` (application property) +
// `set index of w to 1`. The previous `set frontmost of w to true` was
// invalid (frontmost is not a window property) and caused macOS to restore
// the last-active window instead of `w`, explaining "opens but wrong window".
var focusScript = []string{
	`on run argv`,
	`  set target to item 1 of argv`,
	`  tell application "Terminal"`,
	`    activate`,
	`    repeat with w in windows`,
	`      set hits to (tabs of w whose tty is target)`,
	`      if (count of hits) > 0 then`,
	`        if miniaturized of w then set miniaturized of w to false`,
	`        set selected tab of w to item 1 of hits`,
	`        set frontmost to true`,
	`        set index of w to 1`,
	`        return "ok"`,
	`      end if`,
	`    end repeat`,
	`  end tell`,
	`  return "not found"`,
	`end run`,
}

// TerminalApp focuses Terminal.app tabs with AppleScript, matching tabs by tty.
type TerminalApp struct {
	Run Runner
}

func NewTerminalApp() TerminalApp {
	return TerminalApp{Run: execRunner}
}

func (a TerminalApp) CanFocus(pid int) bool {
	if !a.hasTerminalAncestor(pid) {
		debugf("CanFocus pid=%d: no Terminal ancestor", pid)
		return false
	}
	tty, err := a.tty(pid)
	if err != nil {
		debugf("CanFocus pid=%d: tty error: %v", pid, err)
		return false
	}
	debugf("CanFocus pid=%d: tty=%s ok", pid, tty)
	return true
}

func (a TerminalApp) Focus(pid int) error {
	tty, err := a.tty(pid)
	if err != nil {
		debugf("Focus pid=%d: tty error: %v", pid, err)
		return err
	}
	target := "/dev/" + tty
	debugf("Focus pid=%d: tty=%s target=%s", pid, tty, target)
	stdout, stderr, err := run(a.Run, "osascript", scriptArgs(tty)...)
	debugf("Focus pid=%d: osascript err=%v stdout=%q stderr=%q", pid, err, stdout, stderr)
	if err != nil {
		return osascriptError(err, stderr)
	}
	if stdout != "ok" {
		return errors.New("Terminal tab not found. It may have been closed.")
	}
	// Backstop for macOS 26 focus-stealing prevention (spec §7.4 step 3).
	_, stderr2, err2 := run(a.Run, "open", "-b", "com.apple.Terminal")
	debugf("Focus pid=%d: open backstop err=%v stderr=%q", pid, err2, stderr2)
	return nil
}

func scriptArgs(tty string) []string {
	args := make([]string, 0, 2*len(focusScript)+1)
	for _, line := range focusScript {
		args = append(args, "-e", line)
	}
	return append(args, "/dev/"+tty)
}

func osascriptError(err error, stderr string) error {
	switch {
	case strings.Contains(stderr, "(-1743)"):
		return errors.New("Not allowed to control Terminal. Allow it in System Settings → Privacy & Security → Automation, or run `tccutil reset AppleEvents` for the app you started listtt from.")
	case strings.Contains(stderr, "(-10000)"):
		return errors.New("Terminal couldn't be scripted. If the window is in Split View, click it once and try again.")
	default:
		return fmt.Errorf("Terminal focus failed (%v): %s", err, stderr)
	}
}

func (a TerminalApp) tty(pid int) (string, error) {
	out, _, err := run(a.Run, "ps", "-o", "tty=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", fmt.Errorf("read tty of pid %d: %w", pid, err)
	}
	if !macTTY.MatchString(out) {
		return "", fmt.Errorf("pid %d has no Terminal tty (got %q)", pid, out)
	}
	return out, nil
}

// hasTerminalAncestor walks parents with ps. On macOS, comm is the full
// executable path, e.g. /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal.
func (a TerminalApp) hasTerminalAncestor(pid int) bool {
	ppid, _, ok := a.psInfo(pid)
	for hops := 0; ok && ppid > 1 && hops < 64; hops++ {
		var comm string
		var next int
		next, comm, ok = a.psInfo(ppid)
		if !ok {
			return false
		}
		if strings.Contains(comm, "Terminal.app/") {
			return true
		}
		ppid = next
	}
	return false
}

// psInfo returns the parent pid and command path of pid.
func (a TerminalApp) psInfo(pid int) (int, string, bool) {
	out, _, err := run(a.Run, "ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return 0, "", false
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", false
	}
	return ppid, strings.TrimSpace(strings.TrimPrefix(out, fields[0])), true
}
