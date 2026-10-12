package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/scan"
)

// key builds the KeyMsg for a key name, so tests read like keystrokes.
func key(name string) tea.KeyMsg {
	switch name {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// press feeds keys in order and returns the resulting model.
func press(m Model, keys ...string) Model {
	var tm tea.Model = m
	for _, k := range keys {
		tm, _ = tm.Update(key(k))
	}
	return tm.(Model)
}

func testModel(t *testing.T) Model {
	t.Helper()
	sessions := []scan.Session{
		{ID: "h1", Cwd: "/repo/backend", Title: "fix kafka consumer lag", Model: "claude-sonnet-5", Modified: time.Now()},
		{ID: "h2", Cwd: "/repo/backend", Title: "add webhook retry", Model: "claude-opus-5", Modified: time.Now()},
		{ID: "r1", Cwd: "/repo/frontend", Title: "css grid alignment", Model: "claude-haiku-4-5", Modified: time.Now()},
		{ID: "a1", Cwd: "/elsewhere", Title: "jvm heap sizing", Model: "claude-opus-5", Modified: time.Now()},
	}
	m := New(sessions, "/repo/backend", t.TempDir(), []string{"opus[1m]", "opus", "sonnet", "haiku", "fable"})
	m.repoRoot = "/repo" // set directly; the test dirs are not real repositories
	m.theme = themeDark
	useTheme(t, themeDark)
	m.rebuild()
	m.cursor = m.firstSelectable()
	return m
}

func useNoColor(t *testing.T) {
	useColorProfile(t, termenv.Ascii)
}

func useColorProfile(t *testing.T, profile termenv.Profile) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(profile)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func useTheme(t *testing.T, name string) {
	t.Helper()
	previous := struct {
		dim, faint, header, title, selected, marker, accent, warn, aiMark, paneBorder, canvas lipgloss.Style
		canvasBg                                                                              lipgloss.TerminalColor
	}{dim, faint, header, title, selected, marker, accent, warn, aiMark, paneBorder, canvas, canvasBg}
	applyTheme(name)
	t.Cleanup(func() {
		dim, faint, header, title, selected, marker, accent, warn, aiMark, paneBorder, canvas = previous.dim, previous.faint, previous.header, previous.title, previous.selected, previous.marker, previous.accent, previous.warn, previous.aiMark, previous.paneBorder, previous.canvas
		canvasBg = previous.canvasBg
	})
}

func TestOpensOnNewSession(t *testing.T) {
	m := testModel(t)
	r, ok := m.current()
	if !ok || r.kind != rowNew {
		t.Fatalf("picker opened on %v, want the New session row", r.kind)
	}
}

func TestEnterOnNewSessionStartsNew(t *testing.T) {
	m := press(testModel(t), "enter")
	if m.Choice == nil {
		t.Fatal("no choice recorded")
	}
	if m.Choice.Mode != launch.New {
		t.Errorf("Mode = %v, want New", m.Choice.Mode)
	}
}

func TestEnterOnSessionResumesIt(t *testing.T) {
	m := press(testModel(t), "down", "enter")
	if m.Choice == nil {
		t.Fatal("no choice recorded")
	}
	if m.Choice.Mode != launch.Resume {
		t.Errorf("Mode = %v, want Resume", m.Choice.Mode)
	}
	if m.Choice.Session.ID != "h1" {
		t.Errorf("resumed %q, want the first session in this directory", m.Choice.Session.ID)
	}
}

func TestRenderingSanitizesMetadataWithoutChangingOperationalValues(t *testing.T) {
	useNoColor(t)
	controls := []rune{'\x1b', '\a', '\u009d', '\u202e', '\u200b', '\ufeff', '\u00ad', '\u2028', '\u2029', '\v', '\f'}
	hostile := string(controls)
	session := scan.Session{
		ID:       "h1",
		Cwd:      "/evil" + hostile + "cwd",
		Title:    "title" + hostile,
		Branch:   "branch" + hostile,
		Model:    "claude-sonnet-5",
		Preview:  []scan.Turn{{Role: "user", Text: "preview" + hostile}},
		Modified: time.Now(),
	}
	models := []string{"opus" + hostile, "sonnet"}
	m := New([]scan.Session{session}, "/current"+hostile+"cwd", t.TempDir(), models)
	m.width = 140
	m.height = 30
	rawSessions := make([]scan.Session, len(m.sessions))
	for i, session := range m.sessions {
		rawSessions[i] = session
		rawSessions[i].Preview = append([]scan.Turn(nil), session.Preview...)
	}
	rawModels := append([]string(nil), m.models...)

	out := press(m, "down").View() // render the session path, branch, model, and preview
	for _, control := range controls {
		if strings.ContainsRune(out, control) {
			t.Errorf("rendered metadata contains control rune %#U:\n%s", control, out)
		}
	}
	for _, want := range []string{"evil  cwd", "branch", "preview", "sonnet"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered session metadata missing %q:\n%s", want, out)
		}
	}
	if !reflect.DeepEqual(m.sessions, rawSessions) || !reflect.DeepEqual(m.models, rawModels) {
		t.Fatal("rendering changed raw operational values")
	}

	newChoice := press(m, "enter")
	if newChoice.Choice == nil || newChoice.Choice.Model != models[0] {
		t.Fatalf("new-session model = %+v, want raw configured model %q", newChoice.Choice, models[0])
	}

	resumed := press(m, "down", "enter")
	if resumed.Choice == nil || !reflect.DeepEqual(resumed.Choice.Session, session) {
		t.Fatalf("resumed session = %+v, want original %+v", resumed.Choice, session)
	}
}

func TestRenderingSanitizesMetadataSinks(t *testing.T) {
	useNoColor(t)
	const esc = "\x1b"
	cases := []struct {
		name  string
		want  string
		setup func(Model) Model
	}{
		{
			name: "parent title",
			want: "fork of parent title",
			setup: func(m Model) Model {
				m.sessions[0].Title = "parent" + esc + " title"
				m.rebuild()
				return press(m, "down", "down")
			},
		},
		{
			name: "parent ID fallback",
			want: "fork of parent",
			setup: func(m Model) Model {
				m.sessions[0].Title = ""
				m.sessions[0].ID = "parent" + esc
				m.sessions[1].ParentID = m.sessions[0].ID
				m.rebuild()
				return press(m, "down", "down")
			},
		},
		{
			name: "delete confirmation",
			want: "delete target",
			setup: func(m Model) Model {
				m.sessions[0].Title = "delete target" + esc
				m.rebuild()
				return press(m, "down", "d")
			},
		},
		{
			name: "status",
			want: "status",
			setup: func(m Model) Model {
				m.status = "status" + esc
				return m
			},
		},
		{
			name: "command-line search query",
			want: "search query",
			setup: func(m Model) Model {
				m.SetQuery("query" + esc)
				return m
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New([]scan.Session{
				{ID: "parent", Cwd: "/repo", Title: "parent", Modified: time.Now()},
				{ID: "child", Cwd: "/repo", ParentID: "parent", Title: "child", Modified: time.Now()},
			}, "/repo", t.TempDir(), []string{"opus"})
			m.width = 140
			m.height = 30
			out := tc.setup(m).View()
			if !strings.Contains(out, tc.want) {
				t.Errorf("rendered sink missing %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, esc) {
				t.Fatalf("rendered sink contains OSC introducer:\n%s", out)
			}
		})
	}
}

func TestNavigationSkipsHeaders(t *testing.T) {
	m := testModel(t)
	// Walk the whole list; every stop must be selectable.
	for range len(m.rows) + 2 {
		m = press(m, "down")
		r, ok := m.current()
		if !ok || !r.selectable() {
			t.Fatalf("cursor landed on a non-selectable row (%v)", r.kind)
		}
	}
}

func TestNavigationStopsAtEnds(t *testing.T) {
	m := testModel(t)
	first := m.cursor
	if got := press(m, "up", "up", "up").cursor; got != first {
		t.Errorf("cursor = %d after pressing up at the top, want %d", got, first)
	}

	m = press(m, "G")
	last := m.cursor
	if got := press(m, "down", "down").cursor; got != last {
		t.Errorf("cursor = %d after pressing down at the bottom, want %d", got, last)
	}
}

func TestModelFollowsHighlightedSession(t *testing.T) {
	m := press(testModel(t), "down") // onto the sonnet session
	if got := m.currentModel(); got != "sonnet" {
		t.Errorf("model = %q, want the session's own model", got)
	}
	m = press(m, "down") // onto the opus session
	if got := m.currentModel(); got != "opus" {
		t.Errorf("model = %q, want opus", got)
	}
}

func TestNewSessionRowUsesConfiguredDefault(t *testing.T) {
	m := press(testModel(t), "down", "up") // onto a session, then back to New
	if got := m.currentModel(); got != "opus[1m]" {
		t.Errorf("model = %q, want the configured default", got)
	}
}

func TestExplicitModelChoiceSticks(t *testing.T) {
	// Once the user picks a model, moving the cursor must not overwrite it.
	m := press(testModel(t), "right")
	chosen := m.currentModel()
	m = press(m, "down", "down")
	if got := m.currentModel(); got != chosen {
		t.Errorf("model = %q after moving, want the pinned %q", got, chosen)
	}
}

func TestModelCyclingWraps(t *testing.T) {
	m := testModel(t)
	n := len(m.models)
	start := m.currentModel()
	for range n {
		m = press(m, "right")
	}
	if got := m.currentModel(); got != start {
		t.Errorf("model = %q after a full cycle, want %q", got, start)
	}
	if got := press(testModel(t), "left").currentModel(); got != m.models[n-1] {
		t.Errorf("left from the first model = %q, want %q", got, m.models[n-1])
	}
}

func TestMissingDirectoryRefusesLaunch(t *testing.T) {
	m := New([]scan.Session{{
		ID: "gone", Cwd: "/no/such/dir", Title: "old tmp project", Missing: true,
		Modified: time.Now(),
	}}, "/repo/backend", t.TempDir(), []string{"opus"})
	m = press(m, "down", "enter")
	if m.Choice != nil {
		t.Fatal("resumed a session whose directory is gone")
	}
	if m.status == "" {
		t.Error("want an explanation when the directory is gone")
	}
}

func TestYankCopiesSessionID(t *testing.T) {
	m := press(testModel(t), "y")
	if m.status == "" && m.Choice != nil {
		t.Fatal("yank on New session should not launch")
	}
	m = press(testModel(t), "down", "y")
	if !strings.Contains(m.status, "copied") && !strings.Contains(m.status, "could not copy") {
		t.Errorf("status = %q, want a copy result", m.status)
	}
	if m.Choice != nil {
		t.Fatal("yank must not launch")
	}
}

func TestForkRequiresASession(t *testing.T) {
	m := press(testModel(t), "f") // still on the New session row
	if m.Choice != nil {
		t.Fatal("fork acted on the New session row")
	}
	if m.status == "" {
		t.Error("want an explanation when fork has nothing to fork")
	}

	m = press(testModel(t), "down", "f")
	if m.Choice == nil || m.Choice.Mode != launch.Fork {
		t.Errorf("Choice = %+v, want a Fork", m.Choice)
	}
}

func TestDeleteAsksBeforeActing(t *testing.T) {
	m := press(testModel(t), "down", "d")
	if !m.confirming {
		t.Fatal("delete did not ask for confirmation")
	}
	if n := len(m.sessions); n != 4 {
		t.Errorf("sessions = %d, want all 4 still present before confirming", n)
	}
}

func TestDeleteCancelledByAnyOtherKey(t *testing.T) {
	m := press(testModel(t), "down", "d", "n")
	if m.confirming {
		t.Error("confirmation still open")
	}
	if len(m.sessions) != 4 {
		t.Errorf("sessions = %d, want 4 after cancelling", len(m.sessions))
	}
}

func TestDeleteMovesTranscriptToTrash(t *testing.T) {
	claudeDir := t.TempDir()
	project := filepath.Join(claudeDir, "projects", "-repo-backend")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(project, "h1.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New([]scan.Session{{
		ID: "h1", Cwd: "/repo/backend", Title: "fix kafka consumer lag", Path: transcript,
		Modified: time.Now(),
	}}, "/repo/backend", claudeDir, []string{"opus"})

	m = press(m, "down", "d", "y")

	if len(m.sessions) != 0 {
		t.Errorf("sessions = %d, want the deleted one gone from the list", len(m.sessions))
	}
	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Error("transcript is still in the store")
	}
	// Moved, not erased: it must be findable in the trash.
	var found bool
	filepath.Walk(filepath.Join(claudeDir, scan.TrashDir), func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && !fi.IsDir() {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("transcript was not preserved in the trash")
	}
}

func TestSearchFiltersAndEscapeRestores(t *testing.T) {
	m := press(testModel(t), "/", "k", "a", "f", "k", "a")
	if !m.searching {
		t.Fatal("not in search mode")
	}
	var shown []string
	for _, r := range m.rows {
		if r.kind == rowSession {
			shown = append(shown, r.session.ID)
		}
	}
	if len(shown) != 1 || shown[0] != "h1" {
		t.Errorf("search showed %v, want just h1", shown)
	}

	m = press(m, "esc")
	if m.searching || m.query != "" {
		t.Errorf("escape left searching=%v query=%q", m.searching, m.query)
	}
	var all int
	for _, r := range m.rows {
		if r.kind == rowSession {
			all++
		}
	}
	if all != 4 {
		t.Errorf("rows after escape = %d sessions, want 4", all)
	}
}

func TestSearchBackspace(t *testing.T) {
	m := press(testModel(t), "/", "k", "a", "backspace")
	if m.query != "k" {
		t.Errorf("query = %q, want %q", m.query, "k")
	}
}

func TestSearchEnterLaunchesMatch(t *testing.T) {
	m := press(testModel(t), "/", "h", "e", "a", "p", "down", "enter")
	if m.Choice == nil {
		t.Fatal("no choice recorded")
	}
	if m.Choice.Session.ID != "a1" {
		t.Errorf("launched %q, want the matching session a1", m.Choice.Session.ID)
	}
}

func TestSearchWithNoMatchesLeavesNewSessionUsable(t *testing.T) {
	m := press(testModel(t), "/", "z", "z", "z", "q", "x")
	r, ok := m.current()
	if !ok || r.kind != rowNew {
		t.Fatalf("cursor on %v, want the New session row to remain selectable", r.kind)
	}
	m = press(m, "enter")
	if m.Choice == nil || m.Choice.Mode != launch.New {
		t.Errorf("Choice = %+v, want a New session", m.Choice)
	}
}

func TestQuitRecordsNoChoice(t *testing.T) {
	if m := press(testModel(t), "q"); m.Choice != nil {
		t.Errorf("Choice = %+v, want none after quitting", m.Choice)
	}
}

func TestEmptyStoreStillOffersNewSession(t *testing.T) {
	m := New(nil, "/repo/backend", t.TempDir(), []string{"opus"})
	r, ok := m.current()
	if !ok || r.kind != rowNew {
		t.Fatalf("cursor on %v, want the New session row", r.kind)
	}
	if m := press(m, "enter"); m.Choice == nil || m.Choice.Mode != launch.New {
		t.Errorf("Choice = %+v, want a New session", m.Choice)
	}
}

func TestAITitleDotStaysInTitleColumn(t *testing.T) {
	m := New([]scan.Session{{
		ID: "h1", Cwd: "/repo/backend", Title: "short title", AITitled: true,
		Modified: time.Now(),
	}}, "/repo/backend", t.TempDir(), []string{"opus"})
	m.theme = themeDark
	useTheme(t, themeDark)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	out := tm.(Model).View()
	if !strings.Contains(out, "short title") || !strings.Contains(out, "·") {
		t.Fatalf("missing title or dot:\n%s", out)
	}
	// The AI-title mark must not sit in its own gutter before Path.
	if strings.Contains(out, "· │") || strings.Contains(out, "·│") {
		t.Errorf("title mark leaked into the Path gutter:\n%s", out)
	}
}

func TestColumnHeaderFitsOnOneLine(t *testing.T) {
	m := testModel(t)
	m.theme = themeLight
	useTheme(t, themeLight)
	inner := 80
	hdr := m.renderColumnHeader(inner)
	if strings.Contains(hdr, "\n") {
		t.Fatalf("header wrapped:\n%s", hdr)
	}
	if lipgloss.Width(hdr) > inner {
		t.Errorf("header is %d cells, inner is %d", lipgloss.Width(hdr), inner)
	}
}

func TestViewHasColumnHeader(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	out := tm.(Model).View()
	if !strings.Contains(out, "Title") || !strings.Contains(out, "│") {
		t.Errorf("missing column header:\n%s", out)
	}
	if !strings.Contains(out, "Age") || !strings.Contains(out, "Model") {
		t.Errorf("missing Age/Model columns:\n%s", out)
	}
}

func TestToolCycleReloadsSessions(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	dir := filepath.Join(home, ".grok", "sessions", "%2Frepo", "g1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := `{"generated_title":"grok session","info":{"id":"g1","cwd":"/repo"},"updated_at":"2026-09-04T01:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.AttachTools(home, "claude", []string{"claude", "grok"})
	m = press(m, "t")
	if m.currentTool() != "grok" {
		t.Errorf("tool = %q, want grok", m.currentTool())
	}
	var ids []string
	for _, r := range m.rows {
		if r.kind == rowSession {
			ids = append(ids, r.session.ID)
		}
	}
	if len(ids) != 1 || ids[0] != "g1" {
		t.Errorf("after cycle, sessions = %v, want g1", ids)
	}
}

func TestToolKeyHiddenWithOneBackend(t *testing.T) {
	m := press(testModel(t), "t")
	if m.currentTool() != "claude" {
		t.Errorf("single-tool picker cycled to %q", m.currentTool())
	}
}

func TestViewWrapsLongTitle(t *testing.T) {
	m := New([]scan.Session{{
		ID: "long", Cwd: "/repo/backend",
		Title:    "Nessus Windows to Linux migration inventory for the whole fleet",
		Modified: time.Now(),
	}}, "/repo/backend", t.TempDir(), []string{"opus"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	out := tm.(Model).View()
	if !strings.Contains(out, "Nessus Windows to Linux") {
		t.Fatalf("missing title start:\n%s", out)
	}
	if !strings.Contains(out, "inventory") {
		t.Errorf("title was clipped instead of wrapping:\n%s", out)
	}
}

func TestSearchKeepsParentOfMatchingFork(t *testing.T) {
	m := New([]scan.Session{
		{ID: "parent", Cwd: "/repo/backend", Title: "original nessus work", Modified: time.Now()},
		{ID: "child", Cwd: "/repo/backend", Title: "forked inventory pass", ParentID: "parent", Modified: time.Now().Add(-time.Hour)},
	}, "/repo/backend", t.TempDir(), []string{"opus"})
	m = press(m, "/", "i", "n", "v", "e", "n")
	var ids []string
	var depths []int
	for _, r := range m.rows {
		if r.kind == rowSession {
			ids = append(ids, r.session.ID)
			depths = append(depths, r.depth)
		}
	}
	if len(ids) != 2 || ids[0] != "parent" || ids[1] != "child" {
		t.Errorf("rows = %v, want parent then child", ids)
	}
	if len(depths) == 2 && (depths[0] != 0 || depths[1] != 1) {
		t.Errorf("depths = %v, want 0 then 1", depths)
	}
}

func TestViewNestsForkUnderParent(t *testing.T) {
	m := New([]scan.Session{
		{ID: "parent", Cwd: "/repo/backend", Title: "main chat", Modified: time.Now()},
		{ID: "child", Cwd: "/repo/backend", Title: "forked chat", ParentID: "parent", Modified: time.Now().Add(-time.Hour)},
	}, "/repo/backend", t.TempDir(), []string{"opus"})
	out := m.View()
	if !strings.Contains(out, "└─ ") && !strings.Contains(out, "├─ ") {
		t.Errorf("fork was not nested under parent:\n%s", out)
	}
	parentAt := strings.Index(out, "main chat")
	childAt := strings.Index(out, "forked chat")
	if parentAt < 0 || childAt < 0 || childAt < parentAt {
		t.Errorf("child should render after parent:\n%s", out)
	}
}

func TestForkMarkAtEndOfNestedTitle(t *testing.T) {
	m := New([]scan.Session{
		{ID: "parent", Cwd: "/repo/backend", Title: "main chat", Modified: time.Now()},
		{ID: "child", Cwd: "/repo/backend", Title: "forked chat", ParentID: "parent", Modified: time.Now().Add(-time.Hour)},
	}, "/repo/backend", t.TempDir(), []string{"opus"})
	m.theme = themeDark
	useTheme(t, themeDark)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	out := tm.(Model).View()
	if !strings.Contains(out, "└─ ") && !strings.Contains(out, "├─ ") {
		t.Fatalf("tree prefix missing:\n%s", out)
	}
	if !strings.Contains(out, "⎇") {
		t.Fatalf("missing fork mark at end of nested row:\n%s", out)
	}
	if strings.Contains(out, "⎇ │") || strings.Contains(out, "⎇│") {
		t.Errorf("fork mark leaked into the Path gutter:\n%s", out)
	}
	parentLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "main chat") {
			parentLine = line
			break
		}
	}
	if strings.Contains(parentLine, "⎇") {
		t.Errorf("root row should not carry the fork mark:\n%s", parentLine)
	}
}

func TestViewRendersWithoutPanicAtManySizes(t *testing.T) {
	sizes := []struct{ w, h int }{{40, 10}, {80, 24}, {120, 40}, {200, 60}, {20, 5}, {60, 12}, {100, 18}}
	for _, sz := range sizes {
		m := testModel(t)
		tm, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		out := tm.(Model).View()
		if out == "" {
			t.Errorf("empty render at %dx%d", sz.w, sz.h)
			continue
		}
		lines := strings.Split(out, "\n")
		if len(lines) > sz.h {
			t.Errorf("%dx%d rendered %d lines", sz.w, sz.h, len(lines))
		}
		for i, line := range lines {
			if lipgloss.Width(line) > sz.w {
				t.Errorf("%dx%d line %d is %d cells: %q", sz.w, sz.h, i, lipgloss.Width(line), line)
				break
			}
		}
	}
}

func TestViewDoesNotOverflowWidth(t *testing.T) {
	for _, theme := range []string{themeDark, themeLight} {
		m := testModel(t)
		m.theme = theme
		useTheme(t, theme)
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
		out := tm.(Model).View()
		for i, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > 140 {
				t.Errorf("theme %s line %d is %d cells (want <= 140)", theme, i, lipgloss.Width(line))
				break
			}
		}
	}
}

func TestViewReflowsOnResize(t *testing.T) {
	m := testModel(t)
	wide, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	wideOut := wide.(Model).View()
	if !strings.Contains(wideOut, "Path") {
		t.Fatalf("wide view missing Path column:\n%s", wideOut)
	}
	narrow, _ := wide.(Model).Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	got := narrow.(Model)
	if got.width != 50 || got.height != 12 {
		t.Errorf("size = %dx%d after resize", got.width, got.height)
	}
	out := got.View()
	if strings.Count(out, "\n")+1 > 12 {
		t.Errorf("narrow view is %d lines, want <= 12", strings.Count(out, "\n")+1)
	}
}

func TestThemeToggleDarkLight(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	t.Setenv("CL_THEME", "")
	m := testModel(t)
	m.theme = themeDark
	useTheme(t, m.theme)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = tm.(Model)
	if !strings.Contains(m.View(), "theme dark") {
		t.Fatalf("want theme dark label:\n%s", m.View())
	}
	m = press(m, "T")
	if m.theme != themeLight {
		t.Errorf("theme = %q, want light", m.theme)
	}
	out := m.View()
	if !strings.Contains(out, "theme light") {
		t.Errorf("want theme light label:\n%s", out)
	}
	if !strings.Contains(out, "T dark/light") {
		t.Errorf("hint should name dark/light:\n%s", out)
	}
}

func TestAgentCycleHintNamesTheAgents(t *testing.T) {
	m := testModel(t)
	m.AttachTools(t.TempDir(), "claude", []string{"claude", "grok", "codex"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	out := tm.(Model).View()
	if !strings.Contains(out, "t claude/grok/codex") {
		t.Errorf("hint should name the agents:\n%s", out)
	}
	if !strings.Contains(out, "agent") || !strings.Contains(out, "claude") {
		t.Errorf("pane should label the current agent:\n%s", out)
	}
}

func TestViewRendersEveryState(t *testing.T) {
	base := testModel(t)
	states := map[string]Model{
		"default":   base,
		"searching": press(base, "/", "k"),
		"confirm":   press(base, "down", "d"),
		"on repo":   press(base, "down", "down", "down"),
		"no preview": func() Model {
			m := press(base, "p")
			return m
		}(),
	}
	for name, m := range states {
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		if out := tm.(Model).View(); out == "" {
			t.Errorf("empty render in state %q", name)
		}
	}
}

func TestShortModel(t *testing.T) {
	tests := map[string]string{
		"claude-sonnet-5":  "sonnet",
		"claude-opus-4-7":  "opus",
		"claude-haiku-4-5": "haiku",
		"<synthetic>":      "", // internal record, nothing launchable
		"":                 "",
	}
	for in, want := range tests {
		if got := shortModel(in); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBogusWindowSizeIgnored(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tm, _ = tm.(Model).Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	got := tm.(Model)
	if got.width != 120 || got.height != 40 {
		t.Errorf("size = %dx%d, want the last good 120x40", got.width, got.height)
	}
}

func TestFramePaneMatchesRequestedWidth(t *testing.T) {
	useTheme(t, themeDark)
	for _, outer := range []int{60, 98, 140} {
		out := framePane(strings.Repeat("─", outer-4), outer, 6)
		var widest int
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > widest {
				widest = w
			}
		}
		if widest != outer {
			t.Errorf("outer %d rendered %d cells", outer, widest)
		}
	}
}

func TestLightFramePanePaintsShortRows(t *testing.T) {
	useColorProfile(t, termenv.TrueColor)
	useTheme(t, themeLight)
	out := framePane("short", 60, 8)
	if !strings.Contains(out, "48;5;255") {
		t.Fatal("light pane padding must use the white canvas background")
	}
	if i := strings.Index(out, "\x1b[0m   "); i >= 0 {
		end := i + 40
		if end > len(out) {
			end = len(out)
		}
		t.Fatalf("unstyled spaces after reset (black ribbon):\n%q", out[i:end])
	}
}

func TestLightThemeDoesNotLeaveBlackRibbons(t *testing.T) {
	useColorProfile(t, termenv.TrueColor)
	m := testModel(t)
	m.theme = themeLight
	useTheme(t, themeLight)
	m.AttachTools(t.TempDir(), "codex", []string{"claude", "grok", "codex"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	out := tm.(Model).View()
	if i := strings.Index(out, "\x1b[0m   "); i >= 0 {
		start := i - 30
		if start < 0 {
			start = 0
		}
		end := i + 40
		if end > len(out) {
			end = len(out)
		}
		t.Fatalf("light theme left unstyled spaces after a reset (black ribbon):\n%q", out[start:end])
	}
}

func TestClampOffsetShrinksWhenWindowGrows(t *testing.T) {
	var sessions []scan.Session
	for i := 0; i < 40; i++ {
		sessions = append(sessions, scan.Session{
			ID:       fmt.Sprintf("s%d", i),
			Cwd:      "/repo/backend",
			Title:    fmt.Sprintf("session number %02d", i),
			Modified: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	m := New(sessions, "/repo/backend", t.TempDir(), []string{"opus"})
	m.theme = themeDark
	useTheme(t, themeDark)
	m.rebuild()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = press(tm.(Model), "G")
	if m.offset == 0 {
		t.Fatal("expected a scrolled offset after jumping to the last row")
	}
	old := m.offset
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = tm.(Model)
	if m.offset >= old {
		t.Errorf("offset stayed %d after the window grew", m.offset)
	}
}

func TestListShowsMoreIndicators(t *testing.T) {
	var sessions []scan.Session
	for i := 0; i < 30; i++ {
		sessions = append(sessions, scan.Session{
			ID:       fmt.Sprintf("s%d", i),
			Cwd:      "/repo/backend",
			Title:    fmt.Sprintf("session number %02d", i),
			Modified: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	m := New(sessions, "/repo/backend", t.TempDir(), []string{"opus"})
	m.theme = themeDark
	useTheme(t, themeDark)
	m.rebuild()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = tm.(Model)
	out := m.View()
	if !strings.Contains(out, "↓") || !strings.Contains(out, "more") {
		t.Fatalf("missing down-more indicator:\n%s", out)
	}
	m = press(m, "G")
	out = m.View()
	if !strings.Contains(out, "↑") {
		t.Errorf("missing up-more indicator after scrolling:\n%s", out)
	}
}

func TestHighlightTurkishIDoesNotSliceMidRune(t *testing.T) {
	out := highlight("İstanbul session", []string{"i"}, title, accent)
	if !strings.Contains(out, "stanbul") {
		t.Errorf("highlight dropped text: %q", out)
	}
}

func TestAgentHintKeepsCopyAndDelete(t *testing.T) {
	m := testModel(t)
	m.AttachTools(t.TempDir(), "claude", []string{"claude", "grok"})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	out := tm.(Model).View()
	for _, want := range []string{"y copy id", "d delete", "pgup/pgdn", "t claude/grok"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in hint:\n%s", want, out)
		}
	}
}

func TestToolCycleListErrorSetsStatus(t *testing.T) {
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	m := testModel(t)
	// home has no .grok/sessions directory, so listing grok fails.
	m.AttachTools(t.TempDir(), "claude", []string{"claude", "grok"})
	m = press(m, "t")
	if m.currentTool() != "grok" {
		t.Errorf("tool = %q, want grok", m.currentTool())
	}
	if m.status == "" {
		t.Fatal("expected a status error when listing grok sessions fails")
	}
}
