package web

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"listtt/internal/dashboard"
)

const port = "7777"

type fakeBackend struct {
	snap         dashboard.Snapshot
	err          error
	createdName  string
	deletedGroup string
	renamed      [2]string
	updatedID    string
	updatedGroup *string
	updatedNote  *string
	deletedSess  string
	focused      string
	unsubscribed chan struct{}
}

func newFakeBackend() *fakeBackend { return &fakeBackend{unsubscribed: make(chan struct{})} }

func (f *fakeBackend) Subscribe() (<-chan dashboard.Snapshot, func()) {
	ch := make(chan dashboard.Snapshot, 1)
	ch <- f.snap
	return ch, func() { close(f.unsubscribed) }
}

func (f *fakeBackend) CreateGroup(name string) (dashboard.GroupView, error) {
	f.createdName = name
	return dashboard.GroupView{ID: "g_1", Name: name}, f.err
}

func (f *fakeBackend) RenameGroup(id, name string) (dashboard.GroupView, error) {
	f.renamed = [2]string{id, name}
	return dashboard.GroupView{ID: id, Name: name}, f.err
}

func (f *fakeBackend) DeleteGroup(id string) error { f.deletedGroup = id; return f.err }

func (f *fakeBackend) UpdateSession(id string, groupID, note *string) (dashboard.SessionView, error) {
	f.updatedID, f.updatedGroup, f.updatedNote = id, groupID, note
	return dashboard.SessionView{ID: id}, f.err
}

func (f *fakeBackend) DeleteSession(id string) error { f.deletedSess = id; return f.err }

func (f *fakeBackend) Focus(id string) error { f.focused = id; return f.err }

func newReq(method, path, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Host = "127.0.0.1:" + port
	if method != http.MethodGet {
		r.Header.Set("Origin", "http://127.0.0.1:"+port)
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errorBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Error string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body %q is not JSON: %v", w.Body.String(), err)
	}
	return body.Error
}

func TestGuardHost(t *testing.T) {
	h := NewHandler(newFakeBackend(), port)
	for host, want := range map[string]int{
		"127.0.0.1:7777":    http.StatusOK,
		"localhost:7777":    http.StatusOK,
		"127.0.0.1:9999":    http.StatusForbidden,
		"evil.example:7777": http.StatusForbidden,
	} {
		r := newReq(http.MethodGet, "/", "")
		r.Host = host
		if got := serve(h, r).Code; got != want {
			t.Errorf("Host %s: status = %d, want %d", host, got, want)
		}
	}
}

func TestGuardOrigin(t *testing.T) {
	fb := newFakeBackend()
	h := NewHandler(fb, port)
	for _, origin := range []string{"http://evil.example", ""} {
		r := newReq(http.MethodPost, "/api/groups", `{"name":"X"}`)
		r.Header.Set("Origin", origin)
		if got := serve(h, r).Code; got != http.StatusForbidden {
			t.Errorf("Origin %q: status = %d, want 403", origin, got)
		}
	}
	if fb.createdName != "" {
		t.Error("backend was called despite a bad Origin")
	}
}

func TestGuardRequiresJSONContentType(t *testing.T) {
	h := NewHandler(newFakeBackend(), port)
	r := newReq(http.MethodPost, "/api/groups", `{"name":"X"}`)
	r.Header.Set("Content-Type", "text/plain")
	if got := serve(h, r).Code; got != http.StatusForbidden {
		t.Errorf("status = %d, want 403", got)
	}
}

func TestServesIndex(t *testing.T) {
	w := serve(NewHandler(newFakeBackend(), port), newReq(http.MethodGet, "/", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<title>listtt</title>") {
		t.Errorf("status = %d, body = %q", w.Code, w.Body.String())
	}
}

func TestCreateGroup(t *testing.T) {
	fb := newFakeBackend()
	w := serve(NewHandler(fb, port), newReq(http.MethodPost, "/api/groups", `{"name":"Frontend"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var g dashboard.GroupView
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil || g.ID != "g_1" || g.Name != "Frontend" {
		t.Errorf("body = %s, err = %v", w.Body, err)
	}
	if fb.createdName != "Frontend" {
		t.Errorf("backend got name %q", fb.createdName)
	}
}

func TestInvalidJSONBody(t *testing.T) {
	w := serve(NewHandler(newFakeBackend(), port), newReq(http.MethodPost, "/api/groups", `{`))
	if w.Code != http.StatusBadRequest || errorBody(t, w) == "" {
		t.Errorf("status = %d, body = %s", w.Code, w.Body)
	}
}

func TestBackendErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{&dashboard.Error{Kind: dashboard.KindValidation, Msg: "bad name"}, http.StatusBadRequest},
		{&dashboard.Error{Kind: dashboard.KindNotFound, Msg: "group not found"}, http.StatusNotFound},
		{&dashboard.Error{Kind: dashboard.KindConflict, Msg: "not now"}, http.StatusConflict},
		{&dashboard.Error{Kind: dashboard.KindStore, Msg: "disk full"}, http.StatusInternalServerError},
		{errors.New("surprise"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		fb := newFakeBackend()
		fb.err = c.err
		w := serve(NewHandler(fb, port), newReq(http.MethodPatch, "/api/groups/g_1", `{"name":"X"}`))
		if w.Code != c.want || errorBody(t, w) != c.err.Error() {
			t.Errorf("%v: status = %d, body = %s; want %d", c.err, w.Code, w.Body, c.want)
		}
	}
}

func TestRenameAndDeleteGroup(t *testing.T) {
	fb := newFakeBackend()
	h := NewHandler(fb, port)
	if w := serve(h, newReq(http.MethodPatch, "/api/groups/g_1", `{"name":"New"}`)); w.Code != http.StatusOK {
		t.Errorf("rename status = %d", w.Code)
	}
	if fb.renamed != [2]string{"g_1", "New"} {
		t.Errorf("renamed = %v", fb.renamed)
	}
	if w := serve(h, newReq(http.MethodDelete, "/api/groups/g_1", "")); w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", w.Code)
	}
	if fb.deletedGroup != "g_1" {
		t.Errorf("deleted = %q", fb.deletedGroup)
	}
}

func TestUpdateSessionOptionalFields(t *testing.T) {
	fb := newFakeBackend()
	h := NewHandler(fb, port)
	if w := serve(h, newReq(http.MethodPatch, "/api/sessions/s1", `{"note":"hi"}`)); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if fb.updatedID != "s1" || fb.updatedGroup != nil || fb.updatedNote == nil || *fb.updatedNote != "hi" {
		t.Errorf("note only: id=%q group=%v note=%v", fb.updatedID, fb.updatedGroup, fb.updatedNote)
	}
	serve(h, newReq(http.MethodPatch, "/api/sessions/s1", `{"groupId":""}`))
	if fb.updatedGroup == nil || *fb.updatedGroup != "" || fb.updatedNote != nil {
		t.Errorf("group only: group=%v note=%v", fb.updatedGroup, fb.updatedNote)
	}
}

func TestDeleteAndFocusSession(t *testing.T) {
	fb := newFakeBackend()
	h := NewHandler(fb, port)
	if w := serve(h, newReq(http.MethodDelete, "/api/sessions/s1", "")); w.Code != http.StatusNoContent || fb.deletedSess != "s1" {
		t.Errorf("delete: status = %d, deleted = %q", w.Code, fb.deletedSess)
	}
	if w := serve(h, newReq(http.MethodPost, "/api/sessions/s1/focus", "")); w.Code != http.StatusNoContent || fb.focused != "s1" {
		t.Errorf("focus: status = %d, focused = %q", w.Code, fb.focused)
	}
	fb.err = &dashboard.Error{Kind: dashboard.KindConflict, Msg: "Terminal tab not found. It may have been closed."}
	if w := serve(h, newReq(http.MethodPost, "/api/sessions/s1/focus", "")); w.Code != http.StatusConflict {
		t.Errorf("focus conflict: status = %d", w.Code)
	}
}

func TestEventsStreamsInitialSnapshotAndUnsubscribes(t *testing.T) {
	fb := newFakeBackend()
	fb.snap = dashboard.Snapshot{Sessions: []dashboard.SessionView{{ID: "s1", State: dashboard.StateWaiting}}}
	srv := httptest.NewUnstartedServer(nil)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	srv.Config.Handler = NewHandler(fb, p)
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
	if !ok {
		t.Fatalf("first line = %q, want a data: event", line)
	}
	var snap dashboard.Snapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil || len(snap.Sessions) != 1 || snap.Sessions[0].ID != "s1" {
		t.Fatalf("snapshot = %+v, err = %v", snap, err)
	}

	resp.Body.Close()
	select {
	case <-fb.unsubscribed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not unsubscribe after the client disconnected")
	}
}
