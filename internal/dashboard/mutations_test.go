package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nya1/listtt/internal/sessions"
	"github.com/nya1/listtt/internal/store"
)

func kindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func strp(s string) *string { return &s }

func TestCreateGroupValidatesName(t *testing.T) {
	d, st, _, _ := newTestDashboard(store.Empty())

	g, err := d.CreateGroup("  Frontend  ")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Name != "Frontend" || !strings.HasPrefix(g.ID, "g_") || len(g.ID) != 18 {
		t.Errorf("group = %+v", g)
	}
	if _, err := d.CreateGroup(strings.Repeat("é", 60)); err != nil {
		t.Errorf("60-character name rejected: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 61), "FRONTEND", "archived", "ARCHIVED"} {
		if _, err := d.CreateGroup(bad); kindOf(err) != KindValidation {
			t.Errorf("CreateGroup(%q) err = %v, want validation error", bad, err)
		}
	}
	if len(st.saves) != 2 {
		t.Errorf("saves = %d, want 2 (only successful creates)", len(st.saves))
	}
	if n := len(d.Snapshot().Groups); n != 3 { // + the synthesized Archived group
		t.Errorf("groups = %d, want 3", n)
	}
}

func TestRenameGroup(t *testing.T) {
	d, _, _, _ := newTestDashboard(store.Empty())
	a, _ := d.CreateGroup("Alpha")
	if _, err := d.CreateGroup("Beta"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RenameGroup(a.ID, "beta"); kindOf(err) != KindValidation {
		t.Errorf("rename to duplicate: err = %v, want validation error", err)
	}
	if g, err := d.RenameGroup(a.ID, "ALPHA"); err != nil || g.Name != "ALPHA" {
		t.Errorf("case-only rename of itself: g = %+v, err = %v", g, err)
	}
	if _, err := d.RenameGroup("g_missing", "X"); kindOf(err) != KindNotFound {
		t.Errorf("rename unknown: err = %v, want not found", err)
	}
}

func TestDeleteGroupMovesSessionsToUngrouped(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	g, _ := d.CreateGroup("Frontend")
	if _, err := d.UpdateSession("s1", &g.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteGroup(g.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	snap := d.Snapshot()
	if len(snap.Groups) != 1 || findView(t, snap, "s1").GroupID != "" { // just the synthesized Archived group
		t.Errorf("snapshot = %+v", snap)
	}
	if err := d.DeleteGroup(g.ID); kindOf(err) != KindNotFound {
		t.Errorf("delete unknown: err = %v, want not found", err)
	}
}

func TestUpdateSession(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	g, _ := d.CreateGroup("Frontend")

	v, err := d.UpdateSession("s1", &g.ID, strp("fixing checkout"))
	if err != nil || v.GroupID != g.ID || v.Note != "fixing checkout" {
		t.Fatalf("v = %+v, err = %v", v, err)
	}
	if v, _ := d.UpdateSession("s1", nil, strp("only note")); v.GroupID != g.ID {
		t.Errorf("nil groupID changed the group: %+v", v)
	}
	if _, err := d.UpdateSession("s1", strp("g_missing"), nil); kindOf(err) != KindValidation {
		t.Errorf("unknown group: err = %v, want validation error", err)
	}
	if _, err := d.UpdateSession("s1", nil, strp(strings.Repeat("x", 10001))); kindOf(err) != KindValidation {
		t.Errorf("long note: err = %v, want validation error", err)
	}
	if _, err := d.UpdateSession("s1", nil, strp(strings.Repeat("é", 10000))); err != nil {
		t.Errorf("10,000-character note rejected: %v", err)
	}
	if _, err := d.UpdateSession("nope", nil, strp("x")); kindOf(err) != KindNotFound {
		t.Errorf("unknown session: err = %v, want not found", err)
	}

	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	if _, err := d.UpdateSession("s1", strp(""), strp("ended but editable")); err != nil {
		t.Errorf("updating an ended session: %v", err)
	}
}

func TestDeleteSessionOnlyArchived(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	if err := d.DeleteSession("s1"); kindOf(err) != KindConflict {
		t.Errorf("delete live: err = %v, want conflict", err)
	}
	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	if err := d.DeleteSession("s1"); kindOf(err) != KindConflict {
		t.Errorf("delete ended: err = %v, want conflict", err)
	}
	d.ApplyPoll(nil, nil, t0.Add(7*24*time.Hour)) // triggers the auto-archive sweep
	if err := d.DeleteSession("s1"); err != nil {
		t.Fatalf("delete archived: %v", err)
	}
	if n := len(d.Snapshot().Sessions); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
	if err := d.DeleteSession("s1"); kindOf(err) != KindNotFound {
		t.Errorf("delete again: err = %v, want not found", err)
	}
}

func TestDeleteSessionRejectsLiveSessionInArchivedGroup(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	archived := archivedGroupID
	if _, err := d.UpdateSession("s1", &archived, nil); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if err := d.DeleteSession("s1"); kindOf(err) != KindConflict {
		t.Errorf("delete live session in Archived: err = %v, want conflict", err)
	}
}

func TestStoreFailureRollsBackMutation(t *testing.T) {
	d, st, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())

	st.err = errors.New("disk full")
	if _, err := d.CreateGroup("X"); kindOf(err) != KindStore {
		t.Fatalf("err = %v, want store error", err)
	}
	if _, err := d.UpdateSession("s1", nil, strp("lost")); kindOf(err) != KindStore {
		t.Fatalf("err = %v, want store error", err)
	}
	snap := d.Snapshot()
	if len(snap.Groups) != 1 || findView(t, snap, "s1").Note != "" { // just the synthesized Archived group
		t.Fatalf("mutations not rolled back: %+v", snap)
	}

	st.err = nil
	if _, err := d.CreateGroup("X"); err != nil {
		t.Errorf("CreateGroup after recovery: %v", err)
	}
}

func TestFocus(t *testing.T) {
	d, _, fo, c := newTestDashboard(store.Empty())
	fo.can[101] = true
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle"), sess("s2", 202, "idle")}, nil, c.now())

	if err := d.Focus("s1"); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if len(fo.focused) != 1 || fo.focused[0] != 101 {
		t.Errorf("focused = %v, want [101]", fo.focused)
	}
	if err := d.Focus("s2"); kindOf(err) != KindConflict {
		t.Errorf("unfocusable: err = %v, want conflict", err)
	}
	fo.err = errors.New("Terminal tab not found. It may have been closed.")
	if err := d.Focus("s1"); kindOf(err) != KindConflict || !strings.Contains(err.Error(), "tab not found") {
		t.Errorf("focuser failure: err = %v", err)
	}
	if err := d.Focus("nope"); kindOf(err) != KindNotFound {
		t.Errorf("unknown: err = %v, want not found", err)
	}
}

func TestMutationBroadcasts(t *testing.T) {
	d, _, _, _ := newTestDashboard(store.Empty())
	ch, cancel := d.Subscribe()
	defer cancel()
	<-ch
	if _, err := d.CreateGroup("G"); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; len(got.Groups) != 2 { // the new group + the synthesized Archived group
		t.Errorf("broadcast snapshot = %+v", got)
	}
}
