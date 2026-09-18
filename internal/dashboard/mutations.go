package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nya1/listtt/internal/store"
)

const (
	maxGroupNameLen = 60
	maxNoteLen      = 10000
)

func newGroupID() string {
	b := make([]byte, 8)
	rand.Read(b) // crypto/rand.Read never fails on supported platforms (Go 1.24+ crashes instead)
	return "g_" + hex.EncodeToString(b)
}

// renameConflictingArchivedGroups renames any real, pre-existing group named
// "Archived" (case-insensitive). CreateGroup/RenameGroup block that name
// going forward, but a store saved before "archived" became reserved could
// already have one, which would otherwise look like a duplicate of the
// synthesized Archived pseudo-group in the sidebar and move dropdowns.
func renameConflictingArchivedGroups(groups []store.Group) (out []store.Group, renamed bool) {
	used := make(map[string]bool, len(groups))
	for _, g := range groups {
		used[strings.ToLower(g.Name)] = true
	}
	for i := range groups {
		if !strings.EqualFold(groups[i].Name, "archived") {
			continue
		}
		delete(used, strings.ToLower(groups[i].Name))
		candidate := groups[i].Name + " (old)"
		for n := 2; used[strings.ToLower(candidate)]; n++ {
			candidate = fmt.Sprintf("%s (old %d)", groups[i].Name, n)
		}
		used[strings.ToLower(candidate)] = true
		groups[i].Name = candidate
		renamed = true
	}
	return groups, renamed
}

// mutateLocked runs fn, saves, and broadcasts. If fn or the save fails, the
// data is restored to what it was before fn ran.
func (d *Dashboard) mutateLocked(fn func() error) error {
	before := d.data.Clone()
	if err := fn(); err != nil {
		d.data = before
		return err
	}
	if err := d.persist.Save(d.data); err != nil {
		d.data = before
		return errorf(KindStore, "saving store failed: %v", err)
	}
	d.unsaved = false
	d.lastSaved = d.now()
	d.broadcastLocked()
	return nil
}

func (d *Dashboard) groupIndexLocked(id string) int {
	for i, g := range d.data.Groups {
		if g.ID == id {
			return i
		}
	}
	return -1
}

func (d *Dashboard) cleanGroupNameLocked(name, exceptID string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxGroupNameLen {
		return "", errorf(KindValidation, "group name must be 1–%d characters", maxGroupNameLen)
	}
	if strings.EqualFold(name, "archived") {
		return "", errorf(KindValidation, "%q is reserved", name)
	}
	for _, g := range d.data.Groups {
		if g.ID != exceptID && strings.EqualFold(g.Name, name) {
			return "", errorf(KindValidation, "a group named %q already exists", g.Name)
		}
	}
	return name, nil
}

func (d *Dashboard) CreateGroup(name string) (GroupView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var g store.Group
	err := d.mutateLocked(func() error {
		clean, err := d.cleanGroupNameLocked(name, "")
		if err != nil {
			return err
		}
		g = store.Group{ID: newGroupID(), Name: clean, CreatedAt: d.now()}
		d.data.Groups = append(d.data.Groups, g)
		return nil
	})
	if err != nil {
		return GroupView{}, err
	}
	return GroupView{ID: g.ID, Name: g.Name}, nil
}

func (d *Dashboard) RenameGroup(id, name string) (GroupView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var g store.Group
	err := d.mutateLocked(func() error {
		i := d.groupIndexLocked(id)
		if i < 0 {
			return errorf(KindNotFound, "group not found")
		}
		clean, err := d.cleanGroupNameLocked(name, id)
		if err != nil {
			return err
		}
		d.data.Groups[i].Name = clean
		g = d.data.Groups[i]
		return nil
	})
	if err != nil {
		return GroupView{}, err
	}
	return GroupView{ID: g.ID, Name: g.Name}, nil
}

func (d *Dashboard) DeleteGroup(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mutateLocked(func() error {
		i := d.groupIndexLocked(id)
		if i < 0 {
			return errorf(KindNotFound, "group not found")
		}
		d.data.Groups = append(d.data.Groups[:i:i], d.data.Groups[i+1:]...)
		for sid, rec := range d.data.Sessions {
			if rec.GroupID == id {
				rec.GroupID = ""
				d.data.Sessions[sid] = rec
			}
		}
		return nil
	})
}

func (d *Dashboard) UpdateSession(id string, groupID, note *string) (SessionView, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := d.mutateLocked(func() error {
		rec, ok := d.data.Sessions[id]
		if !ok {
			return errorf(KindNotFound, "session not found")
		}
		if groupID != nil {
			if *groupID != "" && *groupID != archivedGroupID && d.groupIndexLocked(*groupID) < 0 {
				return errorf(KindValidation, "group not found")
			}
			if rec.GroupID == archivedGroupID && *groupID != archivedGroupID {
				rec.ArchiveExempt = true
			}
			rec.GroupID = *groupID
		}
		if note != nil {
			if utf8.RuneCountInString(*note) > maxNoteLen {
				return errorf(KindValidation, "note must be at most %d characters", maxNoteLen)
			}
			rec.Note = *note
		}
		d.data.Sessions[id] = rec
		return nil
	})
	if err != nil {
		return SessionView{}, err
	}
	return d.viewLocked(id, d.data.Sessions[id]), nil
}

func (d *Dashboard) DeleteSession(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mutateLocked(func() error {
		rec, ok := d.data.Sessions[id]
		if !ok {
			return errorf(KindNotFound, "session not found")
		}
		if rec.GroupID != archivedGroupID || rec.EndedAt == nil {
			return errorf(KindConflict, "only archived sessions can be deleted")
		}
		delete(d.data.Sessions, id)
		return nil
	})
}

// Focus raises the session's terminal tab. The focus command runs without
// holding the lock, because it can take up to 5 s.
func (d *Dashboard) Focus(id string) error {
	d.mu.Lock()
	rec, ok := d.data.Sessions[id]
	var pid int
	var can bool
	if ok {
		can = d.viewLocked(id, rec).CanFocus
		pid = d.live[id].pid
	}
	d.mu.Unlock()

	if !ok {
		return errorf(KindNotFound, "session not found")
	}
	if !can {
		return errorf(KindConflict, "this session's terminal can't be focused")
	}
	if err := d.focus.Focus(pid); err != nil {
		return errorf(KindConflict, "%v", err)
	}
	return nil
}
