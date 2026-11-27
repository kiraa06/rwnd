package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildNewSession(t *testing.T) {
	got := Build("/bin/claude", New, "", "", "/now/here", "opus")
	want := []string{"/bin/claude", "--model", "opus"}
	if !equalArgs(got.Args, want) {
		t.Errorf("Args = %v, want %v", got.Args, want)
	}
	if got.Dir != "/now/here" {
		t.Errorf("Dir = %q, want the current directory", got.Dir)
	}
}

func TestBuildResumeChdirsToSessionDir(t *testing.T) {
	// Resuming elsewhere would run the conversation against the wrong
	// CLAUDE.md, relative paths and git repository.
	got := Build("/bin/claude", Resume, "abc-123", "/repo/backend", "/now/here", "sonnet")
	want := []string{"/bin/claude", "--resume", "abc-123", "--model", "sonnet"}
	if !equalArgs(got.Args, want) {
		t.Errorf("Args = %v, want %v", got.Args, want)
	}
	if got.Dir != "/repo/backend" {
		t.Errorf("Dir = %q, want the session's own directory", got.Dir)
	}
}

func TestBuildFork(t *testing.T) {
	got := Build("/bin/claude", Fork, "abc-123", "/repo", "/now", "opus")
	want := []string{"/bin/claude", "--resume", "abc-123", "--fork-session", "--model", "opus"}
	if !equalArgs(got.Args, want) {
		t.Errorf("Args = %v, want %v", got.Args, want)
	}
}

func TestBuildForGrokAndCodex(t *testing.T) {
	g := BuildFor("grok", "/bin/grok", Fork, "sid", "/repo", "/now", "grok-4.6")
	wantG := []string{"/bin/grok", "--resume", "sid", "--fork-session", "-m", "grok-4.6"}
	if !equalArgs(g.Args, wantG) {
		t.Errorf("grok fork = %v, want %v", g.Args, wantG)
	}
	c := BuildFor("codex", "/bin/codex", Resume, "sid", "/repo", "/now", "gpt-5")
	wantC := []string{"/bin/codex", "resume", "sid", "-c", "model=gpt-5"}
	if !equalArgs(c.Args, wantC) {
		t.Errorf("codex resume = %v, want %v", c.Args, wantC)
	}
	f := BuildFor("codex", "/bin/codex", Fork, "sid", "/repo", "/now", "")
	wantF := []string{"/bin/codex", "fork", "sid"}
	if !equalArgs(f.Args, wantF) {
		t.Errorf("codex fork = %v, want %v", f.Args, wantF)
	}
}

func TestBuildOmitsEmptyModel(t *testing.T) {
	got := Build("/bin/claude", New, "", "", "/now", "")
	if len(got.Args) != 1 {
		t.Errorf("Args = %v, want no --model flag", got.Args)
	}
}

func TestBuildFallsBackToCwdWithoutSessionDir(t *testing.T) {
	got := Build("/bin/claude", Resume, "abc", "", "/now", "opus")
	if got.Dir != "/now" {
		t.Errorf("Dir = %q, want the current directory as fallback", got.Dir)
	}
}

func TestModelsPutsConfiguredDefaultFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus[1m]","other":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	models, idx := Models(path)
	if len(models) == 0 || models[0] != "opus[1m]" {
		t.Fatalf("models = %v, want the configured model first", models)
	}
	if idx != 0 {
		t.Errorf("start index = %d, want 0", idx)
	}
	for _, f := range Families {
		if IndexOf(models, f) < 0 {
			t.Errorf("models %v is missing the %q alias", models, f)
		}
	}
}

func TestModelsWithoutSettings(t *testing.T) {
	models, _ := Models(filepath.Join(t.TempDir(), "absent.json"))
	if !equalArgs(models, Families) {
		t.Errorf("models = %v, want the standard families", models)
	}
}

func TestModelsIgnoresMalformedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if models, _ := Models(path); !equalArgs(models, Families) {
		t.Errorf("models = %v; a broken settings file must not break the picker", models)
	}
}

func TestModelsDoesNotDuplicateDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	models, _ := Models(path)
	var n int
	for _, m := range models {
		if m == "sonnet" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("models = %v, want one sonnet entry", models)
	}
}

func TestAlias(t *testing.T) {
	tests := map[string]string{
		"claude-sonnet-5":   "sonnet",
		"claude-opus-5":     "opus",
		"claude-opus-4-7":   "opus",
		"claude-haiku-4-5":  "haiku",
		"claude-fable-5-1":  "fable",
		"opus":              "opus",
		"some-future-model": "",
		"":                  "",
	}
	for in, want := range tests {
		if got := Alias(in); got != want {
			t.Errorf("Alias(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExecRejectsEmptyInvocation(t *testing.T) {
	if err := Exec(Spec{}); err == nil {
		t.Error("want an error for an empty invocation")
	}
}

func equalArgs(a, b []string) bool {
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
