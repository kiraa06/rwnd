package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/scan"
)

// TestLiveRender renders a frame from the real session store. It asserts the
// picker survives real data, and logs the frame so the layout can be eyeballed
// with `go test -run TestLiveRender -v`.
// envInt reads an integer from the environment, falling back to def.
func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v >= 0 {
		return v
	}
	return def
}

// TestLiveRender renders a frame from a session store. It asserts the picker
// survives real data, and logs the frame so the layout can be eyeballed with
// `go test -run TestLiveRender -v`.
//
// The CL_RENDER_* variables point it at a different store and drive it before
// capturing, which is how the README images are produced; see CONTRIBUTING.md.
func TestLiveRender(t *testing.T) {
	// Rendering to a file is for documentation, and a pipe has no colour
	// profile to detect, so ask for full colour explicitly.
	if os.Getenv("CL_RENDER_OUT") != "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	store := os.Getenv("CL_RENDER_STORE")
	if store == "" {
		store = filepath.Join(home, ".claude", "projects")
	}
	claudeDir := filepath.Dir(store)

	sessions, err := scan.All(store)
	if err != nil {
		t.Skipf("no session store: %v", err)
	}
	if len(sessions) == 0 {
		t.Skip("store is empty")
	}

	cwd := os.Getenv("CL_RENDER_CWD")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	models, _ := launch.Models(filepath.Join(claudeDir, "settings.json"))

	m := New(sessions, cwd, claudeDir, models)
	if repo := os.Getenv("CL_RENDER_REPO"); repo != "" {
		m.repoRoot = repo
		m.rebuild()
		m.cursor = m.firstSelectable()
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: envInt("CL_RENDER_COLS", 130), Height: envInt("CL_RENDER_ROWS", 32)})
	m = tm.(Model)

	frame := m.View()
	if frame == "" {
		t.Fatal("empty frame")
	}
	t.Logf("\n%s\n", frame)

	if out := os.Getenv("CL_RENDER_OUT"); out != "" {
		shot := m
		for range envInt("CL_RENDER_DOWN", 0) {
			shot = press(shot, "down")
		}
		if q := os.Getenv("CL_RENDER_QUERY"); q != "" {
			shot = press(shot, "/")
			for _, r := range q {
				shot = press(shot, string(r))
			}
		}
		if err := os.WriteFile(out, []byte(shot.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Moving down must land on a real session and adopt its model.
	m = press(m, "down")
	r, ok := m.current()
	if !ok || r.kind != rowSession {
		t.Fatalf("second row is %v, want a session", r.kind)
	}
}
