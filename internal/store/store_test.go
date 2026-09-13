package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sampleData() Data {
	ended := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	return Data{
		Version: CurrentVersion,
		Groups:  []Group{{ID: "g_01", Name: "Frontend", CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}},
		Sessions: map[string]SessionRecord{
			"s1": {GroupID: "g_01", Note: "fixing checkout", Name: "app-3d", Cwd: "/home/dev/app", Recap: "fixed the checkout bug", Title: "Fix checkout bug",
				StartedAt:   time.Date(2026, 9, 9, 7, 0, 0, 0, time.UTC),
				FirstSeenAt: time.Date(2026, 9, 9, 7, 0, 3, 0, time.UTC),
				LastSeenAt:  ended, EndedAt: &ended},
			"s2": {Name: "example-b1", Cwd: "/home/dev/example",
				StartedAt:   time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC),
				FirstSeenAt: time.Date(2026, 9, 12, 7, 0, 3, 0, time.UTC),
				LastSeenAt:  time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)},
		},
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	f := File{Path: filepath.Join(t.TempDir(), "nope", "store.json")}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, Empty()) {
		t.Errorf("got %+v, want Empty()", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	f := File{Path: filepath.Join(t.TempDir(), "listtt", "store.json")}
	want := sampleData()
	if err := f.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestSaveCreatesPrivateFilesAndLeavesNoTemp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "listtt")
	f := File{Path: filepath.Join(dir, "store.json")}
	if err := f.Save(sampleData()); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(sampleData()); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	fi, err := os.Stat(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir contains %v, want only store.json", names)
	}
}

func TestLoadCorruptFileFailsAndLeavesItUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	const corrupt = "{not json"
	if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := File{Path: path}.Load()
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want an error naming %s", err, path)
	}
	after, _ := os.ReadFile(path)
	if string(after) != corrupt {
		t.Errorf("file was modified: %q", after)
	}
}

func TestLoadUnknownVersionFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, []byte(`{"version": 2, "groups": [], "sessions": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := File{Path: path}.Load()
	if err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("err = %v, want unsupported version error", err)
	}
}

func TestCloneIsDeep(t *testing.T) {
	orig := sampleData()
	c := orig.Clone()
	c.Groups[0].Name = "changed"
	rec := c.Sessions["s1"]
	*rec.EndedAt = rec.EndedAt.Add(time.Hour)
	rec.Note = "changed"
	c.Sessions["s1"] = rec
	delete(c.Sessions, "s2")

	if !reflect.DeepEqual(orig, sampleData()) {
		t.Errorf("modifying the clone changed the original: %+v", orig)
	}
}
