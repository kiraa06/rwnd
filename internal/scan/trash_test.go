package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrashPreservesRepeatedTranscriptAndSidecar(t *testing.T) {
	store := t.TempDir()
	project := filepath.Join(store, "sessions", "project")
	transcript := filepath.Join(project, "session.jsonl")
	sidecar := strings.TrimSuffix(transcript, ".jsonl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}

	contents := []string{"first", "second"}
	targets := make([]string, 0, len(contents))
	for _, content := range contents {
		if err := os.WriteFile(transcript, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(sidecar, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sidecar, "agent.jsonl"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		target, err := Trash(store, Session{Path: transcript})
		if len(targets) > 0 {
			got, readErr := os.ReadFile(targets[0])
			if readErr != nil || string(got) != contents[0] {
				t.Fatalf("first trashed transcript changed: got %q, %v; want %q", got, readErr, contents[0])
			}
		}
		if err != nil {
			t.Fatalf("trash %s transcript: %v", content, err)
		}
		targets = append(targets, target)
	}

	if filepath.Dir(targets[0]) == filepath.Dir(targets[1]) {
		t.Fatalf("deletions share a directory: %v", targets)
	}
	for i, target := range targets {
		entry := filepath.Dir(target)
		info, err := os.Stat(entry)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("trash entry %s is not private: %v", entry, info.Mode())
		}
		for _, path := range []string{target, filepath.Join(strings.TrimSuffix(target, ".jsonl"), "agent.jsonl")} {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != contents[i] {
				t.Errorf("read %s = %q, %v; want %q", path, got, err, contents[i])
			}
		}
	}
}

func TestTrashContainsDirectoryWithUntrustedID(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "store")
	project := filepath.Join(store, "sessions", "%2Ftmp")
	source := filepath.Join(project, "actual-session")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}

	target, err := Trash(store, Session{
		ID:   "../../../escaped",
		Path: source,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDatedTrashEntry(t, store, target)
	wantName := filepath.Base(project) + "__" + filepath.Base(source)
	if got := filepath.Base(target); got != wantName {
		t.Fatalf("trash name = %q, want filesystem-derived %q", got, wantName)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("trashed session missing at %s: %v", target, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after trash: %v", err)
	}
}

func TestTrashContainsFileBackedSidecarWithUntrustedID(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "store")
	project := filepath.Join(store, "sessions", "project")
	transcript := filepath.Join(project, "rollout-X.jsonl")
	sidecar := strings.TrimSuffix(transcript, ".jsonl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sidecar, 0o700); err != nil {
		t.Fatal(err)
	}

	transcriptTarget, err := Trash(store, Session{ID: "../../../../../pwned", Path: transcript})
	if err != nil {
		t.Fatal(err)
	}
	assertDatedTrashEntry(t, store, transcriptTarget)
	wantSidecar := filepath.Join(filepath.Dir(transcriptTarget), filepath.Base(project)+"__"+filepath.Base(sidecar))
	if _, err := os.Stat(wantSidecar); err != nil {
		t.Fatalf("sidecar was not contained in trash at %s: %v", wantSidecar, err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("sidecar still exists at source: %v", err)
	}
}

func TestTrashReturnsTranscriptPathWhenSidecarMoveFails(t *testing.T) {
	project := t.TempDir()
	transcript := filepath.Join(project, "session.jsonl")
	sidecar := strings.TrimSuffix(transcript, ".jsonl")
	if err := os.Mkdir(sidecar, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("transcript"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The transcript can move here, but the sidecar cannot move into itself.
	store := filepath.Join(sidecar, "store")
	target, err := Trash(store, Session{Path: transcript})
	if err == nil {
		t.Fatal("trash succeeded despite the sidecar move failing")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "transcript" {
		t.Fatalf("returned path %q does not contain the moved transcript: %q, %v", target, got, readErr)
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Fatalf("transcript still at original path: %v", err)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("sidecar missing from original path: %v", err)
	}
}

func assertDatedTrashEntry(t *testing.T, store, target string) {
	t.Helper()
	dayDir := filepath.Dir(filepath.Dir(target))
	if filepath.Dir(dayDir) != filepath.Join(store, TrashDir) {
		t.Fatalf("trash target escaped its dated directory: %s", target)
	}
	if _, err := time.Parse("2006-01-02", filepath.Base(dayDir)); err != nil {
		t.Fatalf("trash target is not in a dated directory: %s", target)
	}
}
