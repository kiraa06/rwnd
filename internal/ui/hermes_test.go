package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kiraa06/rwnd/internal/scan"
)

func TestHermesDoesNotOfferFileTrashOrCLIFork(t *testing.T) {
	dir := t.TempDir()
	m := New([]scan.Session{{ID: "local-1", Title: "A local session", Cwd: dir, Model: "saved", Tool: "hermes"}}, dir, dir, nil)
	m.AttachTools(dir, "hermes", []string{"claude", "grok", "codex", "hermes"})
	m = press(m, "down")
	for _, key := range []string{"f", "d"} {
		got := press(m, key)
		if got.Choice != nil || got.confirming || !strings.Contains(got.status, "Hermes") {
			t.Errorf("%s should show a Hermes-specific explanation without acting: choice=%+v confirming=%v status=%q", key, got.Choice, got.confirming, got.status)
		}
	}
	footer := m.View()
	for _, hint := range []string{"f fork", "d delete", "←→ model"} {
		if strings.Contains(footer, hint) {
			t.Errorf("Hermes footer advertises unsupported action %q", hint)
		}
	}
	if got := press(m, "enter").Choice; got == nil || got.Tool != "hermes" || got.Session.ID != "local-1" {
		t.Errorf("resume choice = %+v", got)
	}
}

func TestTabWithNoCodexModelAlternativeDoesNotPin(t *testing.T) {
	dir := t.TempDir()
	m := New([]scan.Session{{ID: "codex-1", Title: "A local session", Cwd: dir, Model: "saved", Tool: "codex"}}, dir, dir, nil)
	m.AttachTools(dir, "codex", []string{"codex"})
	got := press(m, "down", "tab", "up", "enter").Choice
	if got == nil || got.Model != "" {
		t.Fatalf("new session should retain the backend default, got %+v", got)
	}
}

func TestUnavailableHermesStoreLeavesOtherAgentsUsable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(home, "state.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(nil, home, home, nil)
	m.AttachTools(home, "hermes", []string{"hermes", "claude"})
	m.LoadSessions()
	if !strings.Contains(m.status, "could not list hermes sessions") {
		t.Errorf("unexpected scan status %q", m.status)
	}
	got := press(m, "t", "enter").Choice
	if got == nil || got.Tool != "claude" {
		t.Errorf("could not switch away from invalid store: %+v", got)
	}
}
