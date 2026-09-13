// Package sessions reads running Claude Code sessions from `claude agents --json`.
package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Session is one live interactive Claude Code session.
type Session struct {
	ID        string
	PID       int
	Name      string
	Cwd       string
	Status    string
	StartedAt time.Time
}

// Runner runs a command and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Source lists sessions by running the claude CLI.
type Source struct {
	Run     Runner
	Timeout time.Duration
}

// NewSource returns a Source that runs the real claude CLI with a 10 s timeout.
func NewSource() *Source {
	return &Source{Run: ExecRunner, Timeout: 10 * time.Second}
}

// List runs `claude agents --json` and returns the interactive sessions.
func (s *Source) List(ctx context.Context) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	out, err := s.Run(ctx, "claude", "agents", "--json")
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	list, err := Parse(out)
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	return list, nil
}

// ExecRunner runs a real command. On failure the error includes stderr.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

type rawEntry struct {
	PID       int    `json:"pid"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	StartedAt int64  `json:"startedAt"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

// Parse decodes `claude agents --json` output. It keeps interactive entries
// that have a sessionId, keeps the latest-started entry per sessionId, and
// sorts newest first.
func Parse(data []byte) ([]Session, error) {
	var raw []rawEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	byID := make(map[string]Session, len(raw))
	for _, e := range raw {
		if e.Kind != "interactive" || e.SessionID == "" {
			continue
		}
		s := Session{ID: e.SessionID, PID: e.PID, Name: e.Name, Cwd: e.Cwd, Status: e.Status,
			StartedAt: time.UnixMilli(e.StartedAt)}
		if prev, ok := byID[s.ID]; ok && !s.StartedAt.After(prev.StartedAt) {
			continue
		}
		byID[s.ID] = s
	}
	list := make([]Session, 0, len(byID))
	for _, s := range byID {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].StartedAt.Equal(list[j].StartedAt) {
			return list[i].StartedAt.After(list[j].StartedAt)
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}
