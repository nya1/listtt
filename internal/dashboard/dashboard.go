package dashboard

import (
	"bytes"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/nya1/listtt/internal/sessions"
	"github.com/nya1/listtt/internal/store"
)

const (
	missedPollsToEnd     = 2
	archiveAfter         = 7 * 24 * time.Hour
	lastSeenPersistEvery = 60 * time.Second
)

type liveInfo struct {
	pid      int
	status   string
	canFocus bool
}

type focusKey struct {
	pid       int
	startedAt int64 // unix ms; a reused pid gets a different key
}

type Dashboard struct {
	// Home is the user's home directory, copied into snapshots. Set it before polling starts.
	Home string

	mu         sync.Mutex
	data       store.Data
	persist    Persister
	focus      Focuser
	recap      Recapper
	now        func() time.Time
	live       map[string]liveInfo
	missed     map[string]int
	focusCache map[focusKey]bool
	lastPollAt *time.Time
	pollError  *string
	lastSaved  time.Time
	unsaved    bool
	subs       map[chan Snapshot]struct{}
	lastSent   []byte
}

func New(data store.Data, persist Persister, focus Focuser, recap Recapper, now func() time.Time) *Dashboard {
	if groups, renamed := renameConflictingArchivedGroups(data.Groups); renamed {
		data.Groups = groups
		if err := persist.Save(data); err != nil {
			log.Printf("listtt: renaming pre-existing %q group failed, will retry on next save: %v", "Archived", err)
		}
	}
	return &Dashboard{
		data:       data,
		persist:    persist,
		focus:      focus,
		recap:      recap,
		now:        now,
		live:       map[string]liveInfo{},
		missed:     map[string]int{},
		focusCache: map[focusKey]bool{},
		lastSaved:  now(),
		subs:       map[chan Snapshot]struct{}{},
	}
}

// ApplyPoll merges one poll result. A failed poll only records the error.
// CanFocus runs under the lock, but only once per new process.
func (d *Dashboard) ApplyPoll(list []sessions.Session, pollErr error, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if pollErr != nil {
		msg := pollErr.Error()
		d.pollError = &msg
		d.broadcastLocked()
		return
	}
	d.pollError = nil
	d.lastPollAt = &now

	changed := false
	seen := make(map[string]bool, len(list))
	live := make(map[string]liveInfo, len(list))
	for _, s := range list {
		seen[s.ID] = true
		rec, exists := d.data.Sessions[s.ID]
		if !exists {
			rec = store.SessionRecord{FirstSeenAt: now}
		}
		newRecap, newTitle := d.recap.Get(d.Home, s.Cwd, s.ID)
		if !exists || rec.Name != s.Name || rec.Cwd != s.Cwd || rec.Recap != newRecap || rec.Title != newTitle || !rec.StartedAt.Equal(s.StartedAt) || rec.EndedAt != nil {
			changed = true
		}
		rec.Name, rec.Cwd, rec.Recap, rec.Title, rec.StartedAt, rec.LastSeenAt, rec.EndedAt = s.Name, s.Cwd, newRecap, newTitle, s.StartedAt, now, nil
		d.data.Sessions[s.ID] = rec
		delete(d.missed, s.ID)

		key := focusKey{pid: s.PID, startedAt: s.StartedAt.UnixMilli()}
		can, ok := d.focusCache[key]
		if !ok {
			can = d.focus.CanFocus(s.PID)
			d.focusCache[key] = can
		}
		live[s.ID] = liveInfo{pid: s.PID, status: s.Status, canFocus: can}
	}
	d.live = live

	for id, rec := range d.data.Sessions {
		if seen[id] || rec.EndedAt != nil {
			continue
		}
		d.missed[id]++
		if d.missed[id] >= missedPollsToEnd {
			ended := rec.LastSeenAt
			rec.EndedAt = &ended
			d.data.Sessions[id] = rec
			delete(d.missed, id)
			changed = true
		}
	}

	for id, rec := range d.data.Sessions {
		if rec.EndedAt == nil || rec.GroupID == archivedGroupID || rec.ArchiveExempt {
			continue
		}
		if now.Sub(rec.LastSeenAt) >= archiveAfter {
			rec.GroupID = archivedGroupID
			d.data.Sessions[id] = rec
			changed = true
		}
	}

	if changed || d.unsaved || (len(list) > 0 && now.Sub(d.lastSaved) >= lastSeenPersistEvery) {
		d.saveLocked(now)
	}
	d.broadcastLocked()
}

// saveLocked persists during a poll. Failures are logged and retried on the next poll.
func (d *Dashboard) saveLocked(now time.Time) {
	if err := d.persist.Save(d.data); err != nil {
		log.Printf("listtt: saving store failed, will retry: %v", err)
		d.unsaved = true
		return
	}
	d.unsaved = false
	d.lastSaved = now
}

func (d *Dashboard) Snapshot() Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.snapshotLocked()
}

func (d *Dashboard) snapshotLocked() Snapshot {
	now := d.now()
	snap := Snapshot{
		GeneratedAt: now,
		LastPollAt:  d.lastPollAt,
		PollError:   d.pollError,
		Home:        d.Home,
		Groups:      make([]GroupView, 0, len(d.data.Groups)),
		Sessions:    make([]SessionView, 0, len(d.data.Sessions)),
	}
	for _, g := range d.data.Groups {
		snap.Groups = append(snap.Groups, GroupView{ID: g.ID, Name: g.Name})
	}
	snap.Groups = append(snap.Groups, GroupView{ID: archivedGroupID, Name: "Archived"})
	for id, rec := range d.data.Sessions {
		snap.Sessions = append(snap.Sessions, d.viewLocked(id, rec))
	}
	sort.Slice(snap.Sessions, func(i, j int) bool { return snap.Sessions[i].ID < snap.Sessions[j].ID })
	return snap
}

func (d *Dashboard) viewLocked(id string, rec store.SessionRecord) SessionView {
	v := SessionView{ID: id, Name: rec.Name, Cwd: rec.Cwd, GroupID: rec.GroupID, Note: rec.Note, Recap: rec.Recap, Title: rec.Title,
		StartedAt: rec.StartedAt, LastSeenAt: rec.LastSeenAt}
	if rec.EndedAt != nil {
		ended := *rec.EndedAt
		v.EndedAt = &ended
		v.State = StateEnded
		return v
	}
	info, ok := d.live[id]
	if !ok {
		// Not ended yet but missing from the latest poll (startup, or 1 missed poll).
		v.State, v.RawStatus = StateOther, "unknown"
		return v
	}
	v.RawStatus, v.CanFocus = info.status, info.canFocus
	switch info.status {
	case "waiting":
		v.State = StateWaiting
	case "busy":
		v.State = StateBusy
	case "idle":
		v.State = StateIdle
	default:
		v.State = StateOther
	}
	return v
}

// Subscribe returns a channel that receives the current snapshot now and
// every changed snapshot later. Slow readers only see the latest snapshot.
func (d *Dashboard) Subscribe() (<-chan Snapshot, func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan Snapshot, 1)
	ch <- d.snapshotLocked()
	d.subs[ch] = struct{}{}
	return ch, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if _, ok := d.subs[ch]; ok {
			delete(d.subs, ch)
			close(ch)
		}
	}
}

// broadcastLocked sends the snapshot to subscribers if anything but GeneratedAt changed.
func (d *Dashboard) broadcastLocked() {
	snap := d.snapshotLocked()
	cmp := snap
	cmp.GeneratedAt = time.Time{}
	key, err := json.Marshal(cmp)
	if err == nil && bytes.Equal(key, d.lastSent) {
		return
	}
	d.lastSent = key
	for ch := range d.subs {
		select {
		case <-ch: // drop the unread older snapshot
		default:
		}
		ch <- snap
	}
}
