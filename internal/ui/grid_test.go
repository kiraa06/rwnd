package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiraa06/rwnd/internal/scan"
)

func TestGridSeparatesChatsWithAlignedColumns(t *testing.T) {
	m := testModel(t)
	useNoColor(t)
	for _, width := range []int{46, 96} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := strings.Split(m.renderList(width), "\n")
			columns := m.renderColumnHeader(width)
			for i, line := range lines {
				if !strings.Contains(line, "fix kafka consumer lag") {
					continue
				}
				rule := lines[i+1]
				if lipgloss.Width(rule) != width || !strings.Contains(rule, "┼") || !strings.Contains(lines[i+2], "add webhook retry") {
					t.Fatalf("expected a full-width rule between chats, got %q then %q", rule, lines[i+2])
				}
				for col, char := range []rune(columns) {
					if char == '│' && []rune(rule)[col] != '┼' {
						t.Errorf("junction does not meet the column at cell %d: %q", col, rule)
					}
				}
				return
			}
			t.Fatal("first chat missing")
		})
	}
}

func TestGridKeepsWrappedTitleInOneRow(t *testing.T) {
	dir := t.TempDir()
	m := New([]scan.Session{
		{ID: "long", Cwd: dir, Title: "First conversation continues here", Modified: time.Now()},
		{ID: "short", Cwd: dir, Title: "Next chat"},
	}, dir, dir, nil)
	useNoColor(t)
	lines := strings.Split(m.renderList(42), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "First conversation") {
			continue
		}
		if i+3 >= len(lines) || !strings.Contains(lines[i+1], "continues here") || !strings.Contains(lines[i+2], "┼") || !strings.Contains(lines[i+3], "Next chat") {
			t.Fatalf("want the continuation before the rule, got %q", lines[i:])
		}
		return
	}
	t.Fatal("wrapped chat missing")
}

func TestGridScrollingKeepsSelectionVisible(t *testing.T) {
	useNoColor(t)
	dir := t.TempDir()
	var sessions []scan.Session
	for i := range 16 {
		sessions = append(sessions, scan.Session{ID: fmt.Sprint(i), Cwd: dir,
			Title:    fmt.Sprintf("Chat %02d with a title that wraps onto the next line", i),
			Modified: time.Unix(100-int64(i), 0)})
	}
	for _, height := range []int{10, 14, 23} {
		m := New(sessions, dir, dir, nil)
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: height})
		m = tm.(Model)
		for _, key := range []string{"down", "up"} {
			for range len(sessions) {
				m = press(m, key)
				out := m.renderList(m.listWidth())
				if !strings.Contains(out, "▸") {
					t.Fatalf("height %d after %s: selected row %d is not visible (offset %d):\n%s", height, key, m.cursor, m.offset, out)
				}
				if lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n"); len(lines) > m.listHeight() {
					t.Fatalf("height %d: rendered %d list lines for a budget of %d", height, len(lines), m.listHeight())
				}
			}
		}
	}
}

func TestGridPageUsesVisualLines(t *testing.T) {
	dir := t.TempDir()
	var sessions []scan.Session
	for i := range 24 {
		sessions = append(sessions, scan.Session{ID: fmt.Sprint(i), Cwd: dir, Title: fmt.Sprintf("Chat %02d", i)})
	}
	m := New(sessions, dir, dir, nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = tm.(Model)
	start := m.cursor
	m = press(m, "pgdown")
	if step, want := m.cursor-start, (m.listHeight()-2)/2; step != want {
		t.Errorf("Page Down moved %d chats with one divider per chat, want %d", step, want)
	}
	m = press(m, "pgup")
	if m.cursor != start {
		t.Errorf("Page Up returned to row %d, want %d", m.cursor, start)
	}
}

func TestGridWideTextDoesNotShiftColumns(t *testing.T) {
	dir := t.TempDir()
	m := New([]scan.Session{{ID: "wide", Cwd: dir, Title: strings.Repeat("界", 38), Model: strings.Repeat("界", 8)}}, dir, dir, nil)
	useNoColor(t)
	const width = 48
	columns := m.renderColumnHeader(width)
	for _, line := range strings.Split(m.renderList(width), "\n") {
		if cut := strings.IndexRune(line, '│'); cut >= 0 {
			if got, want := lipgloss.Width(line[:cut]), strings.IndexRune(columns, '│'); got != want {
				t.Errorf("column separator moved to cell %d, want %d: %q", got, want, line)
			}
			if got := lipgloss.Width(line); got != width {
				t.Errorf("row is %d cells, want %d: %q", got, width, line)
			}
		}
	}
}

func TestGridSelectionStaysVisibleWhenFooterGrows(t *testing.T) {
	useNoColor(t)
	dir := t.TempDir()
	var sessions []scan.Session
	for i := range 4 {
		sessions = append(sessions, scan.Session{ID: fmt.Sprint(i), Cwd: dir,
			Title: fmt.Sprintf("Chat %02d with a title that wraps onto a second line", i)})
	}
	m := New(sessions, dir, dir, nil)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 21})
	m = tm.(Model)
	for range sessions {
		m = press(m, "down")
		for _, key := range []string{"/", "d"} {
			if got := press(m, key).View(); !strings.Contains(got, "▸") {
				t.Errorf("selection at row %d disappeared opening %q:\n%s", m.cursor, key, got)
			}
		}
	}
}

func TestGridDeepForkKeepsTitleVisible(t *testing.T) {
	useNoColor(t)
	m := testModel(t)
	m.rows[2].depth = 10
	m.rows[2].display.Title = "Deep fork"
	if out := m.renderRow(2, 36); !strings.Contains(out, "Deep fork") {
		t.Errorf("tree indentation hid the title: %q", out)
	}
}
