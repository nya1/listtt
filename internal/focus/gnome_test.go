package focus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const screenUnderscore = "d8010785_d30c_48d0_9817_f057076a4631"

type fakeProc struct{ root string }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p fakeProc) add(t *testing.T, pid, ppid int, comm string, env ...string) {
	t.Helper()
	dir := filepath.Join(p.root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "stat"), fmt.Sprintf("%d (%s) S %d %d %d 0 -1", pid, comm, ppid, pid, pid))
	writeFile(t, filepath.Join(dir, "comm"), comm+"\n")
	writeFile(t, filepath.Join(dir, "environ"), strings.Join(env, "\x00")+"\x00")
}

func gnomeTree(t *testing.T) fakeProc {
	p := fakeProc{root: t.TempDir()}
	screen := "GNOME_TERMINAL_SCREEN=/org/gnome/Terminal/screen/" + screenUnderscore
	p.add(t, 1535, 1, "systemd")
	p.add(t, 3498, 1535, "gnome-terminal-")
	p.add(t, 1757997, 3498, "zsh", screen)
	p.add(t, 1930044, 1757997, "claude", "HOME=/home/dev", screen)
	return p
}

func TestGnomeCanFocusInsideGnomeTerminal(t *testing.T) {
	g := GnomeTerminal{ProcRoot: gnomeTree(t).root}
	if !g.CanFocus(1930044) {
		t.Error("CanFocus = false, want true")
	}
}

func TestGnomeCannotFocusTmuxWithStaleScreenEnv(t *testing.T) {
	p := fakeProc{root: t.TempDir()}
	screen := "GNOME_TERMINAL_SCREEN=/org/gnome/Terminal/screen/" + screenUnderscore
	p.add(t, 1535, 1, "systemd")
	p.add(t, 5000, 1535, "tmux: server")
	p.add(t, 5001, 5000, "zsh", screen)
	p.add(t, 5002, 5001, "claude", screen)
	if (GnomeTerminal{ProcRoot: p.root}).CanFocus(5002) {
		t.Error("CanFocus = true for a session inside tmux, want false")
	}
}

func TestGnomeCannotFocusWithoutScreenEnv(t *testing.T) {
	p := fakeProc{root: t.TempDir()}
	p.add(t, 1535, 1, "systemd")
	p.add(t, 3498, 1535, "gnome-terminal-")
	p.add(t, 7000, 3498, "claude", "HOME=/home/dev")
	if (GnomeTerminal{ProcRoot: p.root}).CanFocus(7000) {
		t.Error("CanFocus = true without GNOME_TERMINAL_SCREEN, want false")
	}
}

func TestGnomeCannotFocusMissingProcess(t *testing.T) {
	if (GnomeTerminal{ProcRoot: t.TempDir()}).CanFocus(42) {
		t.Error("CanFocus = true for a missing process, want false")
	}
}

func TestParentPIDHandlesParensAndSpacesInComm(t *testing.T) {
	p := fakeProc{root: t.TempDir()}
	dir := filepath.Join(p.root, "10")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "stat"), "10 (odd) name (x)) S 77 10 10 0 -1")
	ppid, err := GnomeTerminal{ProcRoot: p.root}.parentPID(10)
	if err != nil || ppid != 77 {
		t.Errorf("parentPID = %d, %v; want 77", ppid, err)
	}
}

func TestGnomeFocusCallsActivateResult(t *testing.T) {
	var gotName string
	var gotArgs []string
	g := GnomeTerminal{ProcRoot: gnomeTree(t).root, Run: func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		gotName, gotArgs = name, args
		if _, ok := ctx.Deadline(); !ok {
			t.Error("expected the command to have a timeout")
		}
		return []byte("()\n"), nil, nil
	}}
	if err := g.Focus(1930044); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	want := []string{"call", "--session",
		"--dest", "org.gnome.Terminal",
		"--object-path", "/org/gnome/Terminal/SearchProvider",
		"--method", "org.gnome.Shell.SearchProvider2.ActivateResult",
		"'d8010785-d30c-48d0-9817-f057076a4631'", "[]", "0"}
	if gotName != "gdbus" || !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("ran %s %q\nwant gdbus %q", gotName, gotArgs, want)
	}
}

func TestGnomeFocusReportsGdbusStderr(t *testing.T) {
	g := GnomeTerminal{ProcRoot: gnomeTree(t).root, Run: func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("Error: GDBus.Error:org.freedesktop.DBus.Error.ServiceUnknown\n"), errors.New("exit status 1")
	}}
	err := g.Focus(1930044)
	if err == nil || !strings.Contains(err.Error(), "ServiceUnknown") {
		t.Fatalf("err = %v, want it to include gdbus stderr", err)
	}
}

func TestGnomeFocusMissingProcessFails(t *testing.T) {
	g := GnomeTerminal{ProcRoot: t.TempDir(), Run: func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		t.Error("gdbus must not run for a missing process")
		return nil, nil, nil
	}}
	if err := g.Focus(42); err == nil {
		t.Fatal("expected an error")
	}
}
