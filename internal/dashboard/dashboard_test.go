package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nya1/listtt/internal/sessions"
	"github.com/nya1/listtt/internal/store"
)

type fakeStore struct {
	saves []store.Data
	err   error
}

func (f *fakeStore) Save(d store.Data) error {
	if f.err != nil {
		return f.err
	}
	f.saves = append(f.saves, d.Clone())
	return nil
}

type fakeFocus struct {
	can     map[int]bool
	calls   map[int]int
	focused []int
	err     error
}

func (f *fakeFocus) CanFocus(pid int) bool {
	f.calls[pid]++
	return f.can[pid]
}

func (f *fakeFocus) Focus(pid int) error {
	f.focused = append(f.focused, pid)
	return f.err
}

type fakeRecap struct {
	text  map[string]string // recap, keyed by sessionID
	title map[string]string // title, keyed by sessionID
	calls int
}

func (f *fakeRecap) Get(home, cwd, sessionID string) (recap, title string) {
	f.calls++
	return f.text[sessionID], f.title[sessionID]
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (c *clock) add(d time.Duration) time.Time {
	c.t = c.t.Add(d)
	return c.t
}

var t0 = time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

func newTestDashboard(data store.Data) (*Dashboard, *fakeStore, *fakeFocus, *clock) {
	d, st, fo, _, c := newTestDashboardWithRecap(data, nil, nil)
	return d, st, fo, c
}

func newTestDashboardWithRecap(data store.Data, recapText, titleText map[string]string) (*Dashboard, *fakeStore, *fakeFocus, *fakeRecap, *clock) {
	c := &clock{t: t0}
	st := &fakeStore{}
	fo := &fakeFocus{can: map[int]bool{}, calls: map[int]int{}}
	re := &fakeRecap{text: recapText, title: titleText}
	return New(data, st, fo, re, c.now), st, fo, re, c
}

func sess(id string, pid int, status string) sessions.Session {
	return sessions.Session{ID: id, PID: pid, Name: "name-" + id, Cwd: "/work/" + id, Status: status,
		StartedAt: time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)}
}

func findView(t *testing.T, snap Snapshot, id string) SessionView {
	t.Helper()
	for _, v := range snap.Sessions {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("session %q not in snapshot %+v", id, snap.Sessions)
	return SessionView{}
}

func TestNewSessionGoesToUngroupedAndIsSaved(t *testing.T) {
	d, st, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "waiting")}, nil, c.now())

	v := findView(t, d.Snapshot(), "s1")
	if v.GroupID != "" || v.State != StateWaiting || v.RawStatus != "waiting" || v.Name != "name-s1" {
		t.Errorf("view = %+v", v)
	}
	if len(st.saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(st.saves))
	}
	if rec := st.saves[0].Sessions["s1"]; !rec.FirstSeenAt.Equal(t0) || !rec.LastSeenAt.Equal(t0) {
		t.Errorf("saved record = %+v", rec)
	}
}

func TestStatusMapping(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{
		sess("a", 1, "idle"), sess("b", 2, "busy"), sess("c", 3, "waiting"), sess("d", 4, "compacting"),
	}, nil, c.now())
	snap := d.Snapshot()
	want := map[string]State{"a": StateIdle, "b": StateBusy, "c": StateWaiting, "d": StateOther}
	for id, st := range want {
		if got := findView(t, snap, id).State; got != st {
			t.Errorf("%s: state = %q, want %q", id, got, st)
		}
	}
	if raw := findView(t, snap, "d").RawStatus; raw != "compacting" {
		t.Errorf("rawStatus = %q, want compacting", raw)
	}
}

func TestEndedOnlyAfterTwoMissedPolls(t *testing.T) {
	d, _, fo, c := newTestDashboard(store.Empty())
	fo.can[101] = true
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())

	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	if v := findView(t, d.Snapshot(), "s1"); v.EndedAt != nil {
		t.Fatalf("ended after 1 missed poll: %+v", v)
	}

	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	v := findView(t, d.Snapshot(), "s1")
	if v.EndedAt == nil || !v.EndedAt.Equal(t0) {
		t.Fatalf("EndedAt = %v, want lastSeenAt %v", v.EndedAt, t0)
	}
	if v.State != StateEnded || v.CanFocus {
		t.Errorf("view = %+v, want ended and not focusable", v)
	}
}

func TestFailedPollNeverEndsSession(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	d.ApplyPoll(nil, nil, c.add(3*time.Second)) // 1 miss
	for i := 0; i < 5; i++ {
		d.ApplyPoll(nil, errors.New("claude agents --json: timeout"), c.add(3*time.Second))
	}
	snap := d.Snapshot()
	if v := findView(t, snap, "s1"); v.EndedAt != nil {
		t.Fatalf("failed polls ended the session: %+v", v)
	}
	if snap.PollError == nil || !strings.Contains(*snap.PollError, "timeout") {
		t.Errorf("PollError = %v", snap.PollError)
	}
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.add(3*time.Second))
	if snap := d.Snapshot(); snap.PollError != nil {
		t.Errorf("PollError not cleared: %q", *snap.PollError)
	}
}

func TestArchivedAtSevenDays(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	d.ApplyPoll(nil, nil, c.add(3*time.Second))

	c.t = t0.Add(7*24*time.Hour - time.Second)
	if s := findView(t, d.Snapshot(), "s1").State; s != StateEnded {
		t.Errorf("just before 7 days: state = %q, want ended", s)
	}
	c.t = t0.Add(7 * 24 * time.Hour)
	if s := findView(t, d.Snapshot(), "s1").State; s != StateArchived {
		t.Errorf("at 7 days: state = %q, want archived", s)
	}
}

func TestResumedSessionKeepsGroupAndNote(t *testing.T) {
	ended := t0.Add(-10 * 24 * time.Hour)
	data := store.Empty()
	data.Groups = []store.Group{{ID: "g1", Name: "Frontend", CreatedAt: ended}}
	data.Sessions["s1"] = store.SessionRecord{GroupID: "g1", Note: "keep me", Name: "old", LastSeenAt: ended, EndedAt: &ended}

	d, _, _, c := newTestDashboard(data)
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())

	v := findView(t, d.Snapshot(), "s1")
	if v.GroupID != "g1" || v.Note != "keep me" || v.EndedAt != nil || v.State != StateIdle || v.Name != "name-s1" {
		t.Errorf("view = %+v", v)
	}
}

func TestCanFocusComputedOncePerProcess(t *testing.T) {
	d, _, fo, c := newTestDashboard(store.Empty())
	fo.can[101] = true
	for i := 0; i < 3; i++ {
		d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.add(3*time.Second))
	}
	if fo.calls[101] != 1 {
		t.Errorf("CanFocus called %d times, want 1", fo.calls[101])
	}
	if !findView(t, d.Snapshot(), "s1").CanFocus {
		t.Error("CanFocus = false, want true")
	}
}

func TestCanFocusSurvivesOneMissedPoll(t *testing.T) {
	d, _, fo, c := newTestDashboard(store.Empty())
	fo.can[101] = true
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	d.ApplyPoll(nil, nil, c.add(3*time.Second))                                         // 1 missed poll -- not yet ended
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.add(3*time.Second)) // reappears, same pid+startedAt

	if fo.calls[101] != 1 {
		t.Errorf("CanFocus called %d times across a 1-poll gap, want 1", fo.calls[101])
	}
	if !findView(t, d.Snapshot(), "s1").CanFocus {
		t.Error("CanFocus = false after reappearing, want true")
	}
}

func TestRecapAndTitleFlowIntoViewAndSurviveEnding(t *testing.T) {
	d, _, _, _, c := newTestDashboardWithRecap(store.Empty(),
		map[string]string{"s1": "working on the recap feature"},
		map[string]string{"s1": "Add recap feature"})
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())

	if v := findView(t, d.Snapshot(), "s1"); v.Recap != "working on the recap feature" || v.Title != "Add recap feature" {
		t.Errorf("view = %+v, want recap and title set", v)
	}

	d.ApplyPoll(nil, nil, c.add(3*time.Second))
	d.ApplyPoll(nil, nil, c.add(3*time.Second)) // 2 missed polls: ended
	if v := findView(t, d.Snapshot(), "s1"); v.State != StateEnded || v.Recap != "working on the recap feature" || v.Title != "Add recap feature" {
		t.Errorf("view after ending = %+v, want recap and title to survive", v)
	}
}

func TestLastSeenWriteThrottle(t *testing.T) {
	d, st, _, c := newTestDashboard(store.Empty())
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now()) // new session: save 1
	c.t = t0.Add(3 * time.Second)
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "busy")}, nil, c.now())
	c.t = t0.Add(59 * time.Second)
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	if len(st.saves) != 1 {
		t.Fatalf("saves before 60 s = %d, want 1", len(st.saves))
	}
	c.t = t0.Add(60 * time.Second)
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	if len(st.saves) != 2 {
		t.Fatalf("saves at 60 s = %d, want 2", len(st.saves))
	}
}

func TestSaveFailureDuringPollIsRetried(t *testing.T) {
	d, st, _, c := newTestDashboard(store.Empty())
	st.err = errors.New("disk full")
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.now())
	st.err = nil
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.add(3*time.Second))
	if len(st.saves) != 1 {
		t.Fatalf("saves = %d, want the failed save retried once", len(st.saves))
	}
}

func TestSnapshotIncludesHome(t *testing.T) {
	d, _, _, _ := newTestDashboard(store.Empty())
	d.Home = "/home/dev"
	if got := d.Snapshot().Home; got != "/home/dev" {
		t.Errorf("Home = %q, want /home/dev", got)
	}
}

func TestSubscribeReceivesInitialAndChangedSnapshots(t *testing.T) {
	d, _, _, c := newTestDashboard(store.Empty())
	ch, cancel := d.Subscribe()

	if first := <-ch; len(first.Sessions) != 0 {
		t.Fatalf("initial snapshot = %+v", first)
	}
	d.ApplyPoll([]sessions.Session{sess("s1", 101, "idle")}, nil, c.add(3*time.Second))
	if got := <-ch; len(got.Sessions) != 1 {
		t.Fatalf("after poll: %+v", got)
	}

	pollErr := errors.New("boom")
	d.ApplyPoll(nil, pollErr, c.add(3*time.Second))
	if got := <-ch; got.PollError == nil {
		t.Fatal("expected a snapshot with the poll error")
	}
	d.ApplyPoll(nil, pollErr, c.add(3*time.Second))
	select {
	case got := <-ch:
		t.Fatalf("unchanged snapshot was broadcast: %+v", got)
	default:
	}

	cancel()
	if _, ok := <-ch; ok {
		t.Error("channel not closed after unsubscribe")
	}
}
