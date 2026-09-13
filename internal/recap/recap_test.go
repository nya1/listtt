package recap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptPath(t *testing.T) {
	cases := []struct {
		name, home, cwd, sessionID, want string
	}{
		{"plain", "/home/dev", "/home/dev/work/app", "s1",
			"/home/dev/.claude/projects/-home-dev-work-app/s1.jsonl"},
		{"dotted worktree dir", "/home/dev", "/home/dev/app/.claude-worktrees/feature", "s1",
			"/home/dev/.claude/projects/-home-dev-app--claude-worktrees-feature/s1.jsonl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TranscriptPath(c.home, c.cwd, c.sessionID); got != filepath.FromSlash(c.want) {
				t.Errorf("TranscriptPath = %q, want %q", got, c.want)
			}
		})
	}
}

func writeTranscript(t *testing.T, home, cwd, sessionID, content string) string {
	t.Helper()
	path := TranscriptPath(home, cwd, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func awaySummaryLine(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"type": "system", "subtype": "away_summary", "content": text})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func aiTitleLine(t *testing.T, title string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"type": "ai-title", "aiTitle": title, "sessionId": "s1"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestGetMissingFileReturnsEmpty(t *testing.T) {
	r := NewReader()
	recap, title := r.Get(t.TempDir(), "/proj", "missing")
	if recap != "" || title != "" {
		t.Errorf("Get = (%q, %q), want (\"\", \"\")", recap, title)
	}
}

func TestGetReturnsLatestAwaySummary(t *testing.T) {
	home := t.TempDir()
	content := awaySummaryLine(t, "first recap") +
		`{"type":"assistant","content":"unrelated"}` + "\n" +
		awaySummaryLine(t, "second recap")
	writeTranscript(t, home, "/proj", "s1", content)

	r := NewReader()
	if recap, _ := r.Get(home, "/proj", "s1"); recap != "second recap" {
		t.Errorf("recap = %q, want %q", recap, "second recap")
	}
}

func TestGetReturnsLatestTitleAndRecapTogether(t *testing.T) {
	home := t.TempDir()
	content := aiTitleLine(t, "first title") +
		awaySummaryLine(t, "the recap") +
		aiTitleLine(t, "second title")
	writeTranscript(t, home, "/proj", "s1", content)

	r := NewReader()
	recap, title := r.Get(home, "/proj", "s1")
	if recap != "the recap" || title != "second title" {
		t.Errorf("Get = (%q, %q), want (%q, %q)", recap, title, "the recap", "second title")
	}
}

func TestGetIgnoresMalformedLines(t *testing.T) {
	home := t.TempDir()
	content := "not json at all\n" + awaySummaryLine(t, "real recap")
	writeTranscript(t, home, "/proj", "s1", content)

	r := NewReader()
	if recap, _ := r.Get(home, "/proj", "s1"); recap != "real recap" {
		t.Errorf("recap = %q, want %q", recap, "real recap")
	}
}

func TestGetIncrementalAppend(t *testing.T) {
	home := t.TempDir()
	path := writeTranscript(t, home, "/proj", "s1", awaySummaryLine(t, "first recap")+aiTitleLine(t, "first title"))
	r := NewReader()
	recap, title := r.Get(home, "/proj", "s1")
	if recap != "first recap" || title != "first title" {
		t.Fatalf("Get = (%q, %q), want (%q, %q)", recap, title, "first recap", "first title")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(awaySummaryLine(t, "second recap") + aiTitleLine(t, "second title")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	recap, title = r.Get(home, "/proj", "s1")
	if recap != "second recap" || title != "second title" {
		t.Errorf("Get after append = (%q, %q), want (%q, %q)", recap, title, "second recap", "second title")
	}
}

func TestGetDoesNotConsumeUnterminatedLine(t *testing.T) {
	home := t.TempDir()
	partial := strings.TrimSuffix(awaySummaryLine(t, "in-flight recap"), "\n") // no trailing newline yet
	path := writeTranscript(t, home, "/proj", "s1", partial)

	r := NewReader()
	if recap, title := r.Get(home, "/proj", "s1"); recap != "" || title != "" {
		t.Fatalf("Get on partial line = (%q, %q), want empty", recap, title)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if recap, _ := r.Get(home, "/proj", "s1"); recap != "in-flight recap" {
		t.Errorf("recap after completing the line = %q, want %q", recap, "in-flight recap")
	}
}

func TestGetKeysCacheBySessionIDAndCwd(t *testing.T) {
	home := t.TempDir()
	writeTranscript(t, home, "/proj-a", "s1", awaySummaryLine(t, "recap in proj-a"))
	writeTranscript(t, home, "/proj-b", "s1", awaySummaryLine(t, "recap in proj-b"))

	r := NewReader()
	if recap, _ := r.Get(home, "/proj-a", "s1"); recap != "recap in proj-a" {
		t.Fatalf("Get(proj-a) recap = %q, want %q", recap, "recap in proj-a")
	}
	// Same session ID, different cwd (e.g. resumed elsewhere): must not reuse
	// proj-a's cached scan offset against proj-b's unrelated transcript file.
	if recap, _ := r.Get(home, "/proj-b", "s1"); recap != "recap in proj-b" {
		t.Errorf("Get(proj-b) recap = %q, want %q", recap, "recap in proj-b")
	}
	if recap, _ := r.Get(home, "/proj-a", "s1"); recap != "recap in proj-a" {
		t.Errorf("Get(proj-a) after visiting proj-b recap = %q, want %q", recap, "recap in proj-a")
	}
}

func TestGetRescansAfterTruncation(t *testing.T) {
	home := t.TempDir()
	writeTranscript(t, home, "/proj", "s1", awaySummaryLine(t, "old session")+strings.Repeat("x", 200)+"\n")
	r := NewReader()
	if recap, _ := r.Get(home, "/proj", "s1"); recap != "old session" {
		t.Fatalf("recap = %q, want %q", recap, "old session")
	}

	writeTranscript(t, home, "/proj", "s1", awaySummaryLine(t, "new session"))
	if recap, _ := r.Get(home, "/proj", "s1"); recap != "new session" {
		t.Errorf("recap after truncation = %q, want %q", recap, "new session")
	}
}
