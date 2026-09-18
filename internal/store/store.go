// Package store persists groups and per-session notes to a JSON file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CurrentVersion is the only store file version this build understands.
const CurrentVersion = 1

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type SessionRecord struct {
	GroupID       string     `json:"groupId"`
	Note          string     `json:"note"`
	Name          string     `json:"name"`
	Cwd           string     `json:"cwd"`
	Recap         string     `json:"recap"`
	Title         string     `json:"title"`
	StartedAt     time.Time  `json:"startedAt"`
	FirstSeenAt   time.Time  `json:"firstSeenAt"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	EndedAt       *time.Time `json:"endedAt"`
	ArchiveExempt bool       `json:"archiveExempt,omitempty"`
}

type Data struct {
	Version  int                      `json:"version"`
	Groups   []Group                  `json:"groups"`
	Sessions map[string]SessionRecord `json:"sessions"`
}

// Empty returns a store with no groups or sessions.
func Empty() Data {
	return Data{Version: CurrentVersion, Groups: []Group{}, Sessions: map[string]SessionRecord{}}
}

// Clone returns a deep copy, so callers can roll back mutations.
func (d Data) Clone() Data {
	c := Data{
		Version:  d.Version,
		Groups:   append([]Group{}, d.Groups...),
		Sessions: make(map[string]SessionRecord, len(d.Sessions)),
	}
	for id, r := range d.Sessions {
		if r.EndedAt != nil {
			t := *r.EndedAt
			r.EndedAt = &t
		}
		c.Sessions[id] = r
	}
	return c
}

// DefaultPath is <user config dir>/listtt/store.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config dir: %w", err)
	}
	return filepath.Join(dir, "listtt", "store.json"), nil
}

// File is a store backed by one JSON file.
type File struct {
	Path string
}

// Load reads the store. A missing file is an empty store. A corrupt file or an
// unknown version is an error, and the file is left untouched.
func (f File) Load() (Data, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Data{}, fmt.Errorf("read store %s: %w", f.Path, err)
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return Data{}, fmt.Errorf("store %s is not valid JSON; fix or move it (it will not be overwritten): %w", f.Path, err)
	}
	if d.Version != CurrentVersion {
		return Data{}, fmt.Errorf("store %s has unsupported version %d (want %d); it will not be overwritten", f.Path, d.Version, CurrentVersion)
	}
	if d.Groups == nil {
		d.Groups = []Group{}
	}
	if d.Sessions == nil {
		d.Sessions = map[string]SessionRecord{}
	}
	return d, nil
}

// Save writes the store atomically: temp file in the same directory, fsync, rename.
func (f File) Save(d Data) error {
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".store-*.json")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, f.Path); err != nil {
		return fmt.Errorf("replace %s: %w", f.Path, err)
	}
	committed = true
	return nil
}
