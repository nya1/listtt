package sessions

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseKeepsOnlyInteractiveWithSessionID(t *testing.T) {
	data, err := os.ReadFile("testdata/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3: %+v", len(got), got)
	}
	for _, s := range got {
		if s.ID == "1fa22db1-9074-40da-84d9-2c8997a12cfb" {
			t.Error("background session must be skipped")
		}
		if s.Name == "broken-entry" {
			t.Error("entry without sessionId must be skipped")
		}
	}
	m := got[1]
	want := Session{ID: "c2c2c2c2-0000-4000-8000-000000000002", PID: 1930044, Name: "example-b1",
		Cwd: "/home/dev/projects/example", Status: "waiting", StartedAt: time.UnixMilli(1789000000000)}
	if !m.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", m.StartedAt, want.StartedAt)
	}
	m.StartedAt, want.StartedAt = time.Time{}, time.Time{}
	if m != want {
		t.Errorf("got %+v, want %+v", m, want)
	}
}

func TestParseSortsNewestFirst(t *testing.T) {
	data, err := os.ReadFile("testdata/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"listtt-4a", "example-b1", "app-3d"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

func TestParseDuplicateSessionIDKeepsLatestStart(t *testing.T) {
	data := []byte(`[
		{"pid": 2, "kind": "interactive", "sessionId": "dup", "name": "new", "startedAt": 2000, "status": "busy"},
		{"pid": 1, "kind": "interactive", "sessionId": "dup", "name": "old", "startedAt": 1000, "status": "idle"}
	]`)
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != 2 {
		t.Fatalf("got %+v, want only pid 2", got)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestListRunsClaudeAgentsJSON(t *testing.T) {
	var gotName string
	var gotArgs []string
	src := &Source{Timeout: time.Second, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		if _, ok := ctx.Deadline(); !ok {
			t.Error("expected the context to carry the timeout")
		}
		return []byte(`[{"pid": 7, "kind": "interactive", "sessionId": "s1", "name": "n", "startedAt": 1, "status": "idle"}]`), nil
	}}
	got, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "claude" || !reflect.DeepEqual(gotArgs, []string{"agents", "--json"}) {
		t.Errorf("ran %q %v, want claude [agents --json]", gotName, gotArgs)
	}
	if len(got) != 1 || got[0].ID != "s1" {
		t.Errorf("got %+v", got)
	}
}

func TestListWrapsRunnerError(t *testing.T) {
	src := &Source{Timeout: time.Second, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New(`exec: "claude": executable file not found in $PATH`)
	}}
	_, err := src.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "claude agents --json") {
		t.Fatalf("err = %v, want it to mention the command", err)
	}
}
