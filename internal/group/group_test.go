package group

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kiraa06/rwnd/internal/scan"
)

func sess(cwd, title string) scan.Session {
	return scan.Session{ID: title, Cwd: cwd, Title: title}
}

func titles(ss []scan.Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Title
	}
	return out
}

func TestBuildThreeSections(t *testing.T) {
	sessions := []scan.Session{
		sess("/repo/backend", "here-1"),
		sess("/repo/frontend", "repo-1"),
		sess("/repo", "repo-2"),
		sess("/elsewhere", "all-1"),
		sess("/repo/backend", "here-2"),
	}

	got := Build(sessions, "/repo/backend", "/repo", 0)

	if len(got) != 3 {
		t.Fatalf("got %d sections, want 3", len(got))
	}
	if got[0].Kind != KindCwd || len(got[0].Sessions) != 2 {
		t.Errorf("section 0 = %v with %d sessions", got[0].Kind, len(got[0].Sessions))
	}
	if want := []string{"here-1", "here-2"}; !equal(titles(got[0].Sessions), want) {
		t.Errorf("HERE = %v, want %v", titles(got[0].Sessions), want)
	}
	if want := []string{"repo-1", "repo-2"}; !equal(titles(got[1].Sessions), want) {
		t.Errorf("REPO = %v, want %v", titles(got[1].Sessions), want)
	}
	if want := []string{"all-1"}; !equal(titles(got[2].Sessions), want) {
		t.Errorf("ALL = %v, want %v", titles(got[2].Sessions), want)
	}
}

func TestBuildKeepsCwdSectionWhenEmpty(t *testing.T) {
	// The New session row lives in the first section, so it must always exist.
	got := Build(nil, "/repo", "/repo", 0)
	if len(got) != 1 || got[0].Kind != KindCwd {
		t.Fatalf("got %d sections, want just the cwd section", len(got))
	}
}

func TestBuildWithoutRepoHasNoRepoSection(t *testing.T) {
	sessions := []scan.Session{sess("/private/tmp", "here"), sess("/other", "all")}
	got := Build(sessions, "/private/tmp", "", 0)
	for _, s := range got {
		if s.Kind == KindRepo {
			t.Fatal("got a REPO section outside a repository")
		}
	}
	if len(got) != 2 {
		t.Errorf("got %d sections, want 2", len(got))
	}
}

func TestBuildDoesNotLeakSiblingDirectory(t *testing.T) {
	// "/repo-tools" must not be treated as inside "/repo".
	sessions := []scan.Session{sess("/repo-tools", "sibling")}
	got := Build(sessions, "/repo", "/repo", 0)
	last := got[len(got)-1]
	if last.Kind != KindAll || len(last.Sessions) != 1 {
		t.Errorf("sibling directory landed in %v, want ALL", last.Kind)
	}
}

func TestBuildCapKeepsForksWithParent(t *testing.T) {
	sessions := []scan.Session{
		{ID: "p1", Cwd: "/elsewhere", Title: "p1"},
		{ID: "c1", Cwd: "/elsewhere", Title: "c1", ParentID: "p1"},
		{ID: "p2", Cwd: "/elsewhere", Title: "p2"},
		{ID: "c2", Cwd: "/elsewhere", Title: "c2", ParentID: "p2"},
		{ID: "p3", Cwd: "/elsewhere", Title: "p3"},
	}
	got := Build(sessions, "/repo", "", 2)
	all := got[len(got)-1]
	if want := []string{"p1", "c1", "p2", "c2"}; !equal(titles(all.Sessions), want) {
		t.Errorf("ALL = %v, want parent trees kept together, got hidden=%d", titles(all.Sessions), all.Hidden)
	}
	if all.Hidden != 1 {
		t.Errorf("hidden = %d, want 1", all.Hidden)
	}
}

func TestBuildCapsGlobalSection(t *testing.T) {
	var sessions []scan.Session
	for i := range 10 {
		sessions = append(sessions, sess("/elsewhere", string(rune('a'+i))))
	}
	got := Build(sessions, "/repo", "", 4)
	all := got[len(got)-1]
	if len(all.Sessions) != 4 || all.Hidden != 6 {
		t.Errorf("cap gave %d shown / %d hidden, want 4/6", len(all.Sessions), all.Hidden)
	}
}

func TestRepoRootFindsGitDirAndFile(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := RepoRoot(nested); got != repo {
		t.Errorf("RepoRoot(nested) = %q, want %q", got, repo)
	}

	// A linked worktree records .git as a file, and is its own root.
	wt := filepath.Join(base, "worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RepoRoot(wt); got != wt {
		t.Errorf("RepoRoot(worktree) = %q, want %q", got, wt)
	}
}

func TestRepoRootOutsideRepo(t *testing.T) {
	if got := RepoRoot(t.TempDir()); got != "" {
		t.Errorf("RepoRoot = %q, want empty outside a repository", got)
	}
}

func TestAbbreviate(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := Abbreviate(filepath.Join(home, "Documents", "cl")); got != "~/Documents/cl" {
		t.Errorf("Abbreviate = %q, want ~/Documents/cl", got)
	}
	if got := Abbreviate(""); got != "(unknown)" {
		t.Errorf("Abbreviate(\"\") = %q", got)
	}
	long := "/Users/kj/Documents/mis-ets-deployment-tool/.claude/worktrees/determined-jones-ffe268"
	if got := Abbreviate(long); len([]rune(got)) > 46 {
		t.Errorf("Abbreviate did not shorten: %q (%d runes)", got, len([]rune(got)))
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRepoLabelAvoidsRepeatingTheCwd(t *testing.T) {
	sessions := []scan.Session{sess("/repo", "here"), sess("/repo/sub", "repo")}

	// Standing at the repository root: the REPO header must not restate it.
	at := Build(sessions, "/repo", "/repo", 0)
	if got := at[1].Label(); got != "REPO  elsewhere in this repo" {
		t.Errorf("label at repo root = %q", got)
	}

	// Standing in a subdirectory: naming the root is useful.
	below := Build(sessions, "/repo/sub", "/repo", 0)
	if got := below[1].Label(); got != "REPO  /repo" {
		t.Errorf("label below root = %q", got)
	}
}
