package focus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeResult struct {
	stdout, stderr string
	err            error
}

type fakeMac struct {
	t         *testing.T
	ps        map[string]string // key: "<format> <pid>", e.g. "tty= 900"
	osascript fakeResult
	calls     [][]string
}

func (f *fakeMac) run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch name {
	case "ps": // ps -o <format> -p <pid>
		out, ok := f.ps[args[1]+" "+args[3]]
		if !ok {
			return nil, nil, errors.New("exit status 1")
		}
		return []byte(out + "\n"), nil, nil
	case "osascript":
		return []byte(f.osascript.stdout), []byte(f.osascript.stderr), f.osascript.err
	case "open":
		return nil, nil, nil
	}
	f.t.Fatalf("unexpected command %s %q", name, args)
	return nil, nil, nil
}

func (f *fakeMac) ran(name string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == name {
			out = append(out, c[1:])
		}
	}
	return out
}

// claude(900) -> -zsh(899) -> login(898) -> Terminal(700)
func terminalTree(t *testing.T) *fakeMac {
	return &fakeMac{t: t, ps: map[string]string{
		"ppid=,comm= 900": "  899 claude",
		"ppid=,comm= 899": "  898 -zsh",
		"ppid=,comm= 898": "  700 login",
		"ppid=,comm= 700": "    1 /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal",
		"tty= 900":        "ttys003",
	}}
}

func TestTerminalAppCanFocusInsideTerminal(t *testing.T) {
	f := terminalTree(t)
	if !(TerminalApp{Run: f.run}).CanFocus(900) {
		t.Error("CanFocus = false, want true")
	}
}

func TestTerminalAppCannotFocusITerm(t *testing.T) {
	f := terminalTree(t)
	f.ps["ppid=,comm= 700"] = "    1 /Applications/iTerm.app/Contents/MacOS/iTerm2"
	if (TerminalApp{Run: f.run}).CanFocus(900) {
		t.Error("CanFocus = true under iTerm2, want false")
	}
}

func TestTerminalAppCannotFocusWithoutTTY(t *testing.T) {
	f := terminalTree(t)
	f.ps["tty= 900"] = "??"
	if (TerminalApp{Run: f.run}).CanFocus(900) {
		t.Error("CanFocus = true without a tty, want false")
	}
}

func TestTerminalAppFocusPassesTTYOnlyAsArgument(t *testing.T) {
	f := terminalTree(t)
	f.osascript = fakeResult{stdout: "ok\n"}
	if err := (TerminalApp{Run: f.run}).Focus(900); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	calls := f.ran("osascript")
	if len(calls) != 1 {
		t.Fatalf("osascript calls = %d, want 1", len(calls))
	}
	args := calls[0]
	if args[len(args)-1] != "/dev/ttys003" {
		t.Errorf("last argument = %q, want /dev/ttys003", args[len(args)-1])
	}
	if n := strings.Count(strings.Join(args, "\x00"), "-e\x00"); n != len(focusScript) {
		t.Errorf("-e count = %d, want %d", n, len(focusScript))
	}
	for _, a := range args[:len(args)-1] {
		if strings.Contains(a, "ttys003") {
			t.Errorf("tty leaked into the script text: %q", a)
		}
	}
	if opens := f.ran("open"); len(opens) != 1 || strings.Join(opens[0], " ") != "-b com.apple.Terminal" {
		t.Errorf("open calls = %q, want one `open -b com.apple.Terminal`", opens)
	}
}

func TestTerminalAppFocusTabNotFound(t *testing.T) {
	f := terminalTree(t)
	f.osascript = fakeResult{stdout: "not found\n"}
	err := (TerminalApp{Run: f.run}).Focus(900)
	if err == nil || !strings.Contains(err.Error(), "may have been closed") {
		t.Fatalf("err = %v", err)
	}
	if len(f.ran("open")) != 0 {
		t.Error("open must not run when the tab was not found")
	}
}

func TestTerminalAppFocusPermissionDenied(t *testing.T) {
	f := terminalTree(t)
	f.osascript = fakeResult{stderr: "execution error: Not authorized to send Apple events to Terminal. (-1743)", err: errors.New("exit status 1")}
	err := (TerminalApp{Run: f.run}).Focus(900)
	if err == nil || !strings.Contains(err.Error(), "Privacy & Security → Automation") {
		t.Fatalf("err = %v", err)
	}
}

func TestTerminalAppFocusSplitViewError(t *testing.T) {
	f := terminalTree(t)
	f.osascript = fakeResult{stderr: "execution error: Terminal got an error: AppleEvent handler failed. (-10000)", err: errors.New("exit status 1")}
	err := (TerminalApp{Run: f.run}).Focus(900)
	if err == nil || !strings.Contains(err.Error(), "Split View") {
		t.Fatalf("err = %v", err)
	}
}

func TestTerminalAppFocusRejectsUnexpectedTTY(t *testing.T) {
	f := terminalTree(t)
	f.ps["tty= 900"] = `ttys003" & do shell script "x`
	if err := (TerminalApp{Run: f.run}).Focus(900); err == nil {
		t.Fatal("expected an error for an invalid tty")
	}
	if len(f.ran("osascript")) != 0 {
		t.Error("osascript must not run with an invalid tty")
	}
}
