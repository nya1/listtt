# listtt

Local, full-screen web dashboard that lists Claude Code sessions. Users can group
sessions, add notes, see which sessions need input, and click a session to focus
its terminal tab. It is a single Go binary and supports Linux and macOS.

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
