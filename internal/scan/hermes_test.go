package scan

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func hermesStoreFixture(t *testing.T) (string, func(string, ...any)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return path, func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHermesUsesDisplayEligibleMessagesBeforeTitleAndPreviewLimit(t *testing.T) {
	path, exec := hermesStoreFixture(t)
	exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, started_at REAL)`)
	exec(`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL,
		active INTEGER DEFAULT 1, compacted INTEGER DEFAULT 0, display_kind TEXT, display_metadata TEXT)`)
	exec(`INSERT INTO sessions VALUES ('mixed', 'local', 100), ('compacted-only', 'local', 200), ('hidden-only', 'local', 300)`)
	exec(`INSERT INTO messages (session_id, role, content, timestamp, active, compacted, display_kind, display_metadata) VALUES
		('mixed', 'user', 'hidden prompt', 101, 1, 0, 'hidden', NULL),
		('mixed', 'user', 'model-only prompt', 102, 1, 0, NULL, '{"model_only":true}'),
		('mixed', 'user', 'rewound prompt', 103, 0, 0, NULL, NULL),
		('mixed', 'user', 'original prompt', 104, 0, 1, NULL, NULL),
		('mixed', 'assistant', 'archived answer', 105, 0, 1, NULL, NULL),
		('mixed', 'assistant', 'visible with old metadata', 106, 1, 0, NULL, 'invalid JSON'),
		('mixed', 'assistant', 'live answer', 107, 1, 0, NULL, '{"model_only":false}'),
		('compacted-only', 'user', 'compacted prompt', 201, 0, 1, NULL, NULL),
		('hidden-only', 'assistant', 'hidden answer', 301, 0, 1, 'hidden', NULL)`)
	// These newer rows would crowd every visible turn out if filtered after LIMIT.
	for i := range previewTurns {
		exec(`INSERT INTO messages (session_id, role, content, timestamp, display_kind) VALUES
			('mixed', 'assistant', 'hidden answer', ?, 'hidden')`, 108+i)
	}

	sessions, err := Hermes(path)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]Session)
	for _, s := range sessions {
		byID[s.ID] = s
	}
	mixed := byID["mixed"]
	if mixed.Title != "original prompt" {
		t.Errorf("mixed title = %q, want original prompt", mixed.Title)
	}
	wantPreview := []Turn{{"user", "original prompt"}, {"assistant", "archived answer"}, {"assistant", "visible with old metadata"}, {"assistant", "live answer"}}
	if !slices.Equal(mixed.Preview, wantPreview) {
		t.Errorf("mixed preview = %+v, want %+v", mixed.Preview, wantPreview)
	}
	compacted := byID["compacted-only"]
	if compacted.Title != "compacted prompt" || !slices.Equal(compacted.Preview, []Turn{{"user", "compacted prompt"}}) {
		t.Errorf("compacted-only session missing or wrong: %+v", compacted)
	}
	if _, found := byID["hidden-only"]; found {
		t.Error("untitled session with only hidden rows was listed")
	}
}

func TestHermesHidesDelegatedSessionsButKeepsForks(t *testing.T) {
	path, exec := hermesStoreFixture(t)
	exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, title TEXT, parent_session_id TEXT,
		started_at REAL, model_config TEXT)`)
	exec(`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL)`)
	exec(`INSERT INTO sessions VALUES
		('root', 'local', 'Original', NULL, 100, NULL),
		('legacy-fork', 'cli', 'Legacy fork', 'root', 101, 'invalid JSON'),
		('branch', 'cli', 'Branch', 'root', 102, '{"_branched_from":"root","_delegate_from":null}'),
		('reset', 'desktop', 'Reset', 'root', 103, '{"_reset_from":"root"}'),
		('delegate', 'local', 'Delegated work', 'root', 104, '{"_delegate_from":"root"}'),
		('orphaned-delegate', 'cli', 'Detached work', NULL, 105, '{"_delegate_from":"__orphaned__"}')`)

	sessions, err := Hermes(path)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.ID)
		if (s.ID == "branch" || s.ID == "reset" || s.ID == "legacy-fork") && s.ParentID != "root" {
			t.Errorf("%s parent = %q, want root", s.ID, s.ParentID)
		}
	}
	if want := []string{"reset", "branch", "legacy-fork", "root"}; !slices.Equal(ids, want) {
		t.Errorf("listed sessions = %v, want %v", ids, want)
	}
}

func TestHermesReadsActiveLocalSessionsFromWAL(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(t.TempDir(), "profile #1", "state.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`PRAGMA journal_mode = WAL`)
	exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, title TEXT, cwd TEXT, git_branch TEXT, model TEXT, parent_session_id TEXT, started_at REAL, last_activity_at REAL, ended_at REAL, archived INTEGER DEFAULT 0, hidden INTEGER DEFAULT 0)`)
	exec(`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL, active INTEGER DEFAULT 1)`)
	exec(`INSERT INTO sessions VALUES ('parent', 'cli', 'Saved title', ?, 'main', 'saved-model', NULL, 100, 125.25, NULL, 0, 0),
		('child', 'local', NULL, ?, NULL, NULL, 'parent', 200, NULL, NULL, 0, 0),
		('archived', 'desktop', 'Archived', ?, NULL, NULL, NULL, 300, NULL, NULL, 1, 0),
		('hidden', 'local', 'Hidden', ?, NULL, NULL, NULL, 300, NULL, NULL, 0, 1),
		('worker', 'subagent', 'Worker', ?, NULL, NULL, 'parent', 300, NULL, NULL, 0, 0),
		('remote', 'telegram', 'Remote', ?, NULL, NULL, NULL, 300, NULL, NULL, 0, 0),
		('empty', 'local', NULL, ?, NULL, NULL, NULL, 300, NULL, NULL, 0, 0)`, cwd, cwd, cwd, cwd, cwd, cwd, cwd)
	exec(`INSERT INTO messages (session_id, role, content, timestamp, active) VALUES
		('child', 'user', 'An inactive checkpoint', 201, 0),
		('child', 'user', ?, 202, 1),
		('child', 'tool', 'tool output', 203, 1),
		('child', 'assistant', 'Found it', 204, 1)`, "\x00json:"+`[{"type":"text","text":"Find the missing local session"}]`)

	sessions, err := Hermes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("found %d sessions, want child and parent", len(sessions))
	}
	child, parent := sessions[0], sessions[1]
	if child.ID != "child" || child.Title != "Find the missing local session" || child.ParentID != "parent" || child.Cwd != cwd || child.Path != "" || child.Tool != "hermes" || !child.Modified.Equal(time.Unix(204, 0)) {
		t.Errorf("unexpected child: %+v", child)
	}
	if len(child.Preview) != 2 || child.Preview[1] != (Turn{Role: "assistant", Text: "Found it"}) {
		t.Errorf("unexpected preview: %+v", child.Preview)
	}
	if parent.ID != "parent" || parent.Title != "Saved title" || parent.Model != "saved-model" || parent.Branch != "main" || !parent.Modified.Equal(time.Unix(125, 250000000)) {
		t.Errorf("unexpected parent: %+v", parent)
	}
}

func TestHermesMissingStoreDoesNotCreateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if s, err := Hermes(path); len(s) != 0 || err != nil {
		t.Fatalf("missing store: got %v, %v", s, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing store was created: %v", err)
	}
}

func TestHermesReadsLegacySchemaAndLargeMultimodalSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, title TEXT, model TEXT, parent_session_id TEXT, started_at REAL, ended_at REAL)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL)`,
		`INSERT INTO sessions VALUES ('gui-1', 'gui', NULL, NULL, NULL, 100, NULL), ('oneshot-1', 'oneshot', 'Saved one-shot', NULL, NULL, 90, NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	content := "\x00json:" + `[{"type":"text","text":"Explain the fixture image"},{"type":"image_url","image_url":{"url":"` + strings.Repeat("a", 16<<10) + `"}}]`
	if _, err := db.Exec(`INSERT INTO messages VALUES (1, 'gui-1', 'user', ?, 102)`, content); err != nil {
		t.Fatal(err)
	}
	sessions, err := Hermes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "gui-1" || sessions[0].Title != "Explain the fixture image" || sessions[1].ID != "oneshot-1" {
		t.Fatalf("unexpected legacy sessions: %+v", sessions)
	}
}
