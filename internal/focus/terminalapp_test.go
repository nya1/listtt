package focus

import (
	"context"
	"errors"
	"regexp"
	"slices"
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
	f.osascript = fakeResult{stdout: "/dev/ttys003\n"}
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

// The application's `frontmost` is read-only, so the application-scope
// `set frontmost to true` fails with -10006 and aborts the script before the
// window is raised: the right tab gets selected but stays behind.
func TestTerminalAppFocusScriptRaisesMatchedWindow(t *testing.T) {
	script := strings.Join(focusScript, "\n")
	if !strings.Contains(script, "set frontmost of window id wid to true") {
		t.Error("script must raise the matched window with `set frontmost of window id wid to true`")
	}
	if strings.Contains(script, "set frontmost to true") {
		t.Error("`set frontmost to true` sets the read-only application property and fails with -10006")
	}
	if strings.Index(script, "activate") > strings.Index(script, "repeat with w in windows") {
		t.Error("activate must run before the window loop, or macOS restores Terminal's own front window")
	}
}

// `repeat with w in windows` binds w positionally -- "item i of windows",
// re-resolved on every use -- and the raise reorders that list, so a second
// statement on w silently hits a different window. Reading w exactly once, to
// capture the id, makes that impossible by construction. The `whose` query is
// included: it inherits the container it was asked about, so going through w
// there would leave `item 1 of hits` positional too.
func TestTerminalAppFocusScriptReadsLoopVariableOnce(t *testing.T) {
	bareW := regexp.MustCompile(`\bw\b`)
	var reads []string
	for _, line := range focusScript {
		if strings.Contains(line, "repeat with w in windows") || !bareW.MatchString(line) {
			continue
		}
		reads = append(reads, strings.TrimSpace(line))
	}
	want := []string{"set wid to id of w"}
	if !slices.Equal(reads, want) {
		t.Errorf("lines reading the loop variable = %q, want exactly %q", reads, want)
	}
}

// The script must not sleep: the previous round added a 2s activation poll for
// a race that turned out not to be the cause. Any reintroduced wait has to be
// re-justified against commandTimeout, which run() applies per invocation.
func TestTerminalAppFocusScriptDoesNotSleep(t *testing.T) {
	script := strings.Join(focusScript, "\n")
	if strings.Contains(script, "delay ") {
		t.Error("script must not `delay`; it spends the caller's commandTimeout budget")
	}
	if regexp.MustCompile(`repeat \d+ times`).MatchString(script) {
		t.Error("script must not spin a counted repeat loop")
	}
}

// Three wrong diagnoses of this adapter each cost a rebuild-and-eyeball cycle
// because a focus that landed on the wrong tab returned "ok" like any other.
func TestTerminalAppFocusReportsWrongTab(t *testing.T) {
	f := terminalTree(t)
	f.osascript = fakeResult{stdout: "/dev/ttys009\n"}
	err := (TerminalApp{Run: f.run}).Focus(900)
	if err == nil {
		t.Fatal("Focus succeeded despite landing on another tab")
	}
	for _, want := range []string{"/dev/ttys003", "/dev/ttys009"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %s", err, want)
		}
	}
	if len(f.ran("open")) != 0 {
		t.Error("open must not run when the wrong tab was focused")
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
