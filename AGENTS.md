# listtt

Local, full-screen web dashboard that lists Claude Code sessions. Users can group
sessions, add notes, see which sessions need input, and click a session to focus
its terminal tab. It is a single Go binary and supports Linux and macOS.

Published as a public GitHub repo: https://github.com/nya1/listtt

- Toolchain: Go 1.25.
- Dependencies: standard library only unless there is a strong reason.

## Learnings (things that were easy to get wrong)

- Session data comes from `claude agents --json`.
  - Interactive `status` is `idle`, `busy`, or `waiting`. `waiting` means a permission
    prompt or question is open.
  - Background sessions use `state` and have no `pid`.
- A new, untrusted folder makes Claude Code show a trust dialog whose default choice is
  **"No, exit"**. Pressing Enter exits. A session on that dialog is not listed by
  `claude agents --json`.
- `/proc/<pid>/comm` is truncated to 15 characters. The GNOME Terminal server appears
  as `gnome-terminal-`, so match that prefix.
- GNOME Terminal tab D-Bus objects expose no tty. Map a process to its tab with the
  `GNOME_TERMINAL_SCREEN` environment variable, then focus the tab with
  `SearchProvider2.ActivateResult` (convert `_` to `-` in the UUID). This was verified
  to raise the window on Wayland.
- Raising a Terminal.app window needs `activate` **before** the raise, and the raise
  must be scoped to a window: Terminal's *window* class has a settable `frontmost`,
  the *application*'s is read-only, so a bare `set frontmost to true` fails with
  -10006. Confirmed. Either one wrong looks like a focus bug rather than an error:
  `activate` last makes macOS restore Terminal's own front window, and a failed raise
  leaves the right tab selected but behind another window. Nothing here can be
  verified off-macOS -- the unit tests only assert the script text, so changes to the
  script need a real click-test on a Mac.
- `repeat with w in windows` binds `w` **positionally** (`item i of windows`,
  re-resolved on every access) and the raise reorders that list, so a second statement
  on `w` silently targets a different window. Read `w` once to capture `id of w`, then
  address `window id wid` -- including the `whose` query, which inherits the container
  it was asked about. A `TestTerminalAppFocusScriptReadsLoopVariableOnce` guard
  enforces this, because the failure is invisible: every statement succeeds.
- **The focus adapter must verify its own outcome.** The script returns the tty of the
  tab that is actually selected when it finishes, and `Focus` compares it to the one
  it asked for. This exists because three separate diagnoses of one wrong-tab report
  were each wrong, and each cost a rebuild-and-eyeball cycle: the adapter returned a
  hardcoded `"ok"`, so no layer above it could tell a silent failure from a success.
  Refuted theories, so they are not tried a fourth time: an `activate`-vs-raise race
  (Terminal comes forward reliably); the stale positional reference above (real, but
  needs two windows, and the report came from a single-window setup); and Apple's
  macOS 26 activation bug FB21087054 (same reason as the first).
- `claude --resume` keeps the session ID; only `--fork-session` creates a new one.
- Each session's full transcript is a JSONL file at
  `~/.claude/projects/<encoded-cwd>/<sessionId>.jsonl`, where `<encoded-cwd>` is
  the session's cwd with every `/` and `.` replaced by `-`. This is undocumented
  and could change between Claude Code versions. `internal/recap` reads it to
  find `{"type":"system","subtype":"away_summary",...}` lines (the "recap"
  shown when returning to an idle session) -- there's no CLI command for this.

## Commands

```bash
~/go/bin/go1.25.0 test ./... -race          # all unit tests
~/sdk/go1.25.0/bin/gofmt -w . && ~/go/bin/go1.25.0 vet ./...
~/go/bin/go1.25.0 build -o listtt .          # build
XDG_CONFIG_HOME="$(mktemp -d)" ./listtt --addr 127.0.0.1:7788   # run against a throwaway store
```

- The UI is plain ES modules in `internal/web/static/`, embedded with `//go:embed`. There's no build step, and you must rebuild the binary to see UI changes.
- The OS focus adapters (`internal/focus/gnome.go`, `terminalapp.go`) have no build tags, so both are unit-tested on any OS. Only `New()` is per-OS.
