// Package dashboard merges live Claude Code sessions with stored groups and
// notes, derives lifecycle state, and broadcasts snapshots.
package dashboard

import (
	"fmt"
	"time"

	"listtt/internal/store"
)

type State string

const (
	StateWaiting  State = "waiting"
	StateBusy     State = "busy"
	StateIdle     State = "idle"
	StateOther    State = "other"
	StateEnded    State = "ended"
	StateArchived State = "archived"
)

type GroupView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SessionView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Cwd        string     `json:"cwd"`
	GroupID    string     `json:"groupId"`
	Note       string     `json:"note"`
	Recap      string     `json:"recap"`
	Title      string     `json:"title"`
	State      State      `json:"state"`
	RawStatus  string     `json:"rawStatus"`
	CanFocus   bool       `json:"canFocus"`
	StartedAt  time.Time  `json:"startedAt"`
	LastSeenAt time.Time  `json:"lastSeenAt"`
	EndedAt    *time.Time `json:"endedAt"`
}

type Snapshot struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	LastPollAt  *time.Time    `json:"lastPollAt"`
	PollError   *string       `json:"pollError"`
	Home        string        `json:"home"`
	Groups      []GroupView   `json:"groups"`
	Sessions    []SessionView `json:"sessions"`
}

// Kind classifies errors so the web layer can pick an HTTP status.
type Kind int

const (
	KindValidation Kind = iota + 1 // 400
	KindNotFound                   // 404
	KindConflict                   // 409
	KindStore                      // 500
)

type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errorf(kind Kind, format string, args ...any) error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// Persister saves the store. store.File implements it.
type Persister interface {
	Save(store.Data) error
}

// Focuser checks and performs terminal focus. focus.New() implements it.
type Focuser interface {
	CanFocus(pid int) bool
	Focus(pid int) error
}

// Recapper reads a session's latest recap and title. recap.NewReader() implements it.
type Recapper interface {
	Get(home, cwd, sessionID string) (recap, title string)
}
