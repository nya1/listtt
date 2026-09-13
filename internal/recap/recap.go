// Package recap reads two things Claude Code writes into a session's own
// transcript file that aren't exposed by any CLI command: the short
// AI-generated title (its own ai-title), and the "recap" it writes while the
// user is away (its own away_summary).
package recap

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TranscriptPath returns the JSONL transcript file Claude Code writes for a
// session, mirroring the directory-naming scheme under ~/.claude/projects:
// every "/" and "." in cwd becomes "-".
func TranscriptPath(home, cwd, sessionID string) string {
	encoded := strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
	return filepath.Join(home, ".claude", "projects", encoded, sessionID+".jsonl")
}

type entry struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Content string `json:"content"`
	AITitle string `json:"aiTitle"`
}

type cacheEntry struct {
	scanned int64 // bytes of complete (newline-terminated) lines already scanned
	recap   string
	title   string
}

// cacheKey includes cwd alongside sessionID so a session ID resumed from a
// different directory (the dashboard already treats cwd as changeable for a
// given ID) starts its scan over instead of seeking into an unrelated file.
type cacheKey struct {
	sessionID string
	cwd       string
}

// Reader extracts the latest recap and title from each session's transcript
// file, re-reading only the bytes appended since the last call for that
// session.
type Reader struct {
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

func NewReader() *Reader {
	return &Reader{cache: map[cacheKey]cacheEntry{}}
}

// Get returns the latest recap and title for a session, or "" for either
// that hasn't been written yet -- a short session, a transcript that doesn't
// exist yet, or recaps disabled in /config are all silently "no recap".
func (r *Reader) Get(home, cwd, sessionID string) (recap, title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cacheKey{sessionID: sessionID, cwd: cwd}
	prev := r.cache[key]

	f, err := os.Open(TranscriptPath(home, cwd, sessionID))
	if err != nil {
		return prev.recap, prev.title
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return prev.recap, prev.title
	}
	size := info.Size()
	if size < prev.scanned {
		prev = cacheEntry{} // truncated or replaced: rescan from the start
	}
	if size == prev.scanned {
		return prev.recap, prev.title
	}
	if _, err := f.Seek(prev.scanned, 0); err != nil {
		return prev.recap, prev.title
	}

	next := prev
	consumed := prev.scanned
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := reader.ReadString('\n')
		if strings.HasSuffix(line, "\n") {
			consumed += int64(len(line))
			var e entry
			if json.Unmarshal([]byte(line), &e) == nil {
				if e.Type == "system" && e.Subtype == "away_summary" {
					next.recap = e.Content
				} else if e.Type == "ai-title" {
					next.title = e.AITitle
				}
			}
		}
		if err != nil {
			break // EOF; an unterminated trailing line is re-read next time
		}
	}
	next.scanned = consumed
	r.cache[key] = next
	return next.recap, next.title
}
