// Package ui implements the session picker.
package ui

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiraa06/rwnd/internal/group"
	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/scan"
	"github.com/kiraa06/rwnd/internal/tool"
)

// allCap bounds the global section; the rest stay reachable through search.
const allCap = 40

// Choice is what the picker decided to launch.
type Choice struct {
	Mode     launch.Mode
	Session  scan.Session
	Model    string
	Tool     string
	StoreDir string
}

// rowKind distinguishes the flattened list's entries.
type rowKind int

const (
	rowHeader rowKind = iota
	rowNew
	rowSession
	rowNote
)

// row is one line of the flattened list. Headers and notes are rendered but
// never selected, which keeps navigation a simple scan for the next selectable
// row instead of a special case per section.
type row struct {
	kind    rowKind
	text    string
	section group.Kind
	session scan.Session
	depth   int
	last    bool
	count   int          // header: sessions in this section
	context bool         // ancestor shown so a matching fork keeps its parent
	display scan.Session // sanitized copy used only for rendering
}

func (r row) selectable() bool { return r.kind == rowNew || r.kind == rowSession }

// Model is the picker's state.
type Model struct {
	sessions   []scan.Session // everything found, unfiltered
	cwd        string
	repoRoot   string
	claudeDir  string
	displayCwd string
	home       string
	loadedFrom string
	tool       string
	tools      []string // detected tool names; switcher hidden unless len>=2

	rows   []row
	cursor int
	offset int // first visible row, for scrolling

	models         []string // backend options, plus the saved model when needed
	baseModelCount int
	modelIdx       int
	// modelOverride survives moving the cursor, without retaining the previous
	// row's saved model as an unrelated cycle option.
	modelOverride string

	query     string
	searching bool

	showPreview bool
	confirming  bool
	status      string
	theme       string // "dark" or "light"

	width, height int

	// Choice is set when the user picks something; the program then quits and
	// the caller execs it.
	Choice *Choice
}

// New builds a picker over the given sessions.
func New(sessions []scan.Session, cwd, claudeDir string, models []string) Model {
	m := Model{
		sessions:  sessions,
		cwd:       cwd,
		repoRoot:  group.RepoRoot(cwd),
		claudeDir: claudeDir,
		models:    models,

		baseModelCount: len(models),
		displayCwd:     scan.SanitizeDisplayText(cwd),
		showPreview:    true,
		theme:          loadTheme(),
		width:          100,
		height:         30,
	}
	applyTheme(m.theme)
	m.rebuild()
	return m
}

func displaySession(s scan.Session) scan.Session {
	display := s
	display.ID = scan.SanitizeDisplayText(s.ID)
	display.Cwd = scan.SanitizeDisplayText(s.Cwd)
	display.Title = scan.SanitizeDisplayText(s.Title)
	display.Branch = scan.SanitizeDisplayText(s.Branch)
	display.Model = scan.SanitizeDisplayText(s.Model)
	display.ParentID = scan.SanitizeDisplayText(s.ParentID)
	if len(s.Preview) > 0 {
		display.Preview = make([]scan.Turn, len(s.Preview))
		for i, turn := range s.Preview {
			display.Preview[i] = turn
			display.Preview[i].Role = scan.SanitizeDisplayText(turn.Role)
			display.Preview[i].Text = scan.SanitizeDisplayText(turn.Text)
		}
	}
	return display
}

// rebuild recomputes the flattened rows from the current query, keeping the
// cursor on a selectable row.
func (m *Model) rebuild() {
	visible := scan.Filter(m.sessions, m.query)
	matchedIDs := make(map[string]bool, len(visible))
	for _, s := range visible {
		matchedIDs[s.ID] = true
	}
	if m.query != "" {
		visible = scan.WithAncestors(m.sessions, visible)
	}
	sections := group.Build(visible, m.cwd, m.repoRoot, allCap)

	rows := make([]row, 0, len(visible)+len(sections)+2)
	for _, sec := range sections {
		hdr := len(rows)
		rows = append(rows, row{kind: rowHeader, text: scan.SanitizeDisplayText(sec.Label()), section: sec.Kind})
		if sec.Kind == group.KindCwd {
			rows = append(rows, row{kind: rowNew, text: "New session", section: sec.Kind})
		}
		nSess := 0
		for _, n := range group.Order(sec.Sessions) {
			nSess++
			rows = append(rows, row{
				kind:    rowSession,
				session: n.Session,
				display: displaySession(n.Session),
				section: sec.Kind,
				depth:   n.Depth,
				last:    n.Last,
				context: m.query != "" && !matchedIDs[n.Session.ID],
			})
		}
		rows[hdr].count = nSess
		if sec.Hidden > 0 {
			rows = append(rows, row{
				kind:    rowNote,
				text:    scan.SanitizeDisplayText(plural(sec.Hidden, "more session") + " — press / to search"),
				section: sec.Kind,
			})
		}
	}
	m.rows = rows

	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 || !m.rows[max(m.cursor, 0)].selectable() {
		m.cursor = m.firstSelectable()
	}
	m.syncModelToCursor()
	m.clampOffset()
}

func (m Model) firstSelectable() int {
	for i, r := range m.rows {
		if r.selectable() {
			return i
		}
	}
	return 0
}

// move steps the cursor by delta, skipping headers and notes and stopping at
// the ends rather than wrapping.
func (m *Model) move(delta int) {
	i := m.cursor
	for {
		i += delta
		if i < 0 || i >= len(m.rows) {
			return // no selectable row that way; stay put
		}
		if m.rows[i].selectable() {
			m.cursor = i
			m.syncModelToCursor()
			m.clampOffset()
			return
		}
	}
}

// movePage jumps roughly one screen, including wrapped rows and dividers.
func (m *Model) movePage(dir int) {
	remaining, width := max(m.listHeight()-2, 1), m.listWidth()
	next := m.cursor
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		remaining -= m.rowVisualHeight(i, width)
		if remaining < 0 && next != m.cursor {
			break
		}
		if m.rows[i].selectable() {
			next = i
		}
	}
	m.cursor = next
	m.syncModelToCursor()
	m.clampOffset()
}

// syncModelToCursor points the footer at the highlighted session's own model,
// so resuming keeps the model the conversation was using. An explicit choice by
// the user takes precedence.
func (m *Model) syncModelToCursor() {
	m.modelIdx = 0
	// Cap the slice so appending cannot modify another Bubble Tea model.
	n := m.baseModelCount
	m.models = m.models[:n:n]
	if r, ok := m.current(); ok && r.kind == rowSession && r.session.Model != "" && r.session.Model != "<synthetic>" {
		saved, kind := r.session.Model, r.session.Tool
		if kind == "" {
			kind = m.currentTool()
		}
		if kind == tool.Claude && launch.IndexOf(m.models, saved) < 0 {
			if alias := launch.Alias(saved); launch.IndexOf(m.models, alias) >= 0 {
				saved = alias
			}
		}
		m.modelIdx = m.modelOption(saved)
	}
	if m.modelOverride != "" {
		m.modelIdx = m.modelOption(m.modelOverride)
	}
}

func (m *Model) modelOption(model string) int {
	if i := launch.IndexOf(m.models, model); i >= 0 {
		return i
	}
	m.models = append(m.models, model)
	return len(m.models) - 1
}

func (m *Model) cycleModel(delta int) {
	if len(m.models) < 2 {
		return
	}
	m.modelIdx = (m.modelIdx + delta + len(m.models)) % len(m.models)
	m.modelOverride = m.currentModel()
	m.syncModelToCursor()
}

func (m Model) currentModel() string {
	if m.modelIdx < len(m.models) {
		return m.models[m.modelIdx]
	}
	return ""
}

// current returns the highlighted row.
func (m Model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) Init() tea.Cmd { return nil }

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Some terminals report zero before settling; keep the last good size
		// rather than collapsing the list to its minimum.
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.clampOffset()
		return m, nil
	case tea.KeyMsg:
		before := m.footerLines()
		var cmd tea.Cmd
		if m.confirming {
			m, cmd = m.updateConfirm(msg, scan.Trash)
		} else if m.searching {
			m, cmd = m.updateSearch(msg)
		} else {
			m, cmd = m.updateNormal(msg)
		}
		if before != m.footerLines() {
			m.clampOffset()
		}
		return m, cmd
	}
	return m, nil
}

// updateConfirm handles the delete confirmation prompt, which swallows every
// other key so a stray keystroke cannot both confirm and act.
func (m Model) updateConfirm(msg tea.KeyMsg, trash func(string, scan.Session) (string, error)) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.confirming = false
		r, ok := m.current()
		if !ok || r.kind != rowSession {
			return m, nil
		}
		store := m.storeDir()
		dest, err := trash(store, r.session)
		if dest == "" {
			if err != nil {
				m.status = "could not delete: " + err.Error()
			}
			return m, nil
		}
		m.removeSession(r.session.ID)
		if rel, err := filepath.Rel(store, dest); err == nil {
			dest = rel
		}
		m.status = "moved to " + dest
		if err != nil {
			m.status += " (partial): " + err.Error()
		}
		return m, nil
	default:
		m.confirming = false
		m.status = ""
		return m, nil
	}
}

// updateSearch handles keys while the search field has focus.
func (m Model) updateSearch(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching, m.query, m.status = false, "", ""
		m.rebuild()
		return m, nil
	case "enter":
		return m.launch(launch.Resume)
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.rebuild()
		}
		return m, nil
	case "ctrl+u":
		m.query = ""
		m.rebuild()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.cycleModel(1)
		return m, nil
	case "shift+tab":
		m.cycleModel(-1)
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.query += string(msg.Runes)
		if msg.Alt {
			return m, nil
		}
		m.rebuild()
	}
	return m, nil
}

// updateNormal handles keys in the default navigation mode.
func (m Model) updateNormal(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k", "ctrl+p":
		m.move(-1)
	case "down", "j", "ctrl+n":
		m.move(1)
	case "pgdown", "ctrl+d", "ctrl+f":
		m.movePage(1)
	case "pgup", "ctrl+b", "ctrl+u":
		m.movePage(-1)
	case "left", "h", "shift+tab":
		m.cycleModel(-1)
	case "right", "l", "tab":
		m.cycleModel(1)
	case "g", "home":
		m.cursor = m.firstSelectable()
		m.syncModelToCursor()
		m.clampOffset()
	case "G", "end":
		for i := len(m.rows) - 1; i >= 0; i-- {
			if m.rows[i].selectable() {
				m.cursor = i
				break
			}
		}
		m.syncModelToCursor()
		m.clampOffset()
	case "/":
		m.searching, m.status = true, ""
	case "p":
		m.showPreview = !m.showPreview
		m.clampOffset()
	case "t":
		if len(m.tools) >= 2 {
			m.cycleTool()
		}
	case "T":
		m.cycleTheme()
	case "enter":
		return m.launch(launch.Resume)
	case "f":
		if m.currentTool() == tool.Hermes {
			m.status = "Hermes: resume the session, then use /branch"
			break
		}
		if r, ok := m.current(); ok && r.kind == rowSession {
			return m.launch(launch.Fork)
		}
		m.status = "fork needs an existing session"
	case "d":
		if m.currentTool() == tool.Hermes {
			m.status = "Hermes: use native session management to archive chats"
			break
		}
		if r, ok := m.current(); ok && r.kind == rowSession {
			m.confirming = true
		} else {
			m.status = "nothing to delete"
		}
	case "y":
		if r, ok := m.current(); ok && r.kind == rowSession {
			if err := copyText(r.session.ID); err != nil {
				m.status = "could not copy id"
			} else {
				m.status = "copied " + r.session.ID
			}
		} else {
			m.status = "nothing to copy"
		}
	}
	return m, nil
}

// launch records the choice and stops the program. The row decides between a
// new session and a resume, so Enter always does the obvious thing.
func (m Model) launch(mode launch.Mode) (Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if r.kind == rowNew {
		mode = launch.New
	} else if r.session.Missing {
		m.status = "directory gone — " + group.Abbreviate(r.session.Cwd)
		return m, nil
	}
	kind := r.session.Tool
	if kind == "" {
		kind = m.currentTool()
	}
	model := m.currentModel()
	if kind == tool.Hermes {
		model = "" // Hermes restores the model and provider together
	}
	m.Choice = &Choice{Mode: mode, Session: r.session, Model: model, Tool: kind, StoreDir: m.storeDir()}
	return m, tea.Quit
}

// removeSession drops a deleted session and rebuilds the list.
func (m *Model) removeSession(id string) {
	kept := make([]scan.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	m.sessions = kept
	m.rebuild()
}

// SetQuery opens the picker already filtered, as when a query is passed on the
// command line.
func (m *Model) SetQuery(q string) {
	m.query = q
	m.searching = true
	m.rebuild()
}

// AttachTools wires detected backends. The switcher stays hidden unless at
// least two tools are present.
func (m *Model) AttachTools(home, current string, names []string) {
	m.home = home
	m.tool = current
	m.tools = names
}

func (m Model) currentTool() string {
	if m.tool != "" {
		return m.tool
	}
	return tool.Claude
}

func (m Model) storeDir() string {
	if m.loadedFrom != "" {
		return m.loadedFrom
	}
	if m.home != "" {
		return tool.StoreDir(m.currentTool(), m.home)
	}
	return m.claudeDir
}

func (m *Model) cycleTool() {
	next := tool.Next(detectedFromNames(m.tools), m.currentTool())
	if next == m.currentTool() {
		return
	}
	m.tool = next
	tool.SavePreferred(next)
	m.LoadSessions()
}

// LoadSessions opens the active backend without disabling the picker if its
// local store is unavailable. Other installed agents remain selectable.
func (m *Model) LoadSessions() {
	kind := m.currentTool()
	m.loadedFrom = tool.StoreDir(kind, m.home)
	sessions, err := tool.ListStore(kind, m.loadedFrom)
	m.sessions, m.status = sessions, ""
	if err != nil && !os.IsNotExist(err) {
		m.status = "could not list " + kind + " sessions: " + err.Error()
	}
	m.models = tool.Models(kind, m.home)
	m.baseModelCount = len(m.models)
	m.modelOverride = ""
	m.cursor = 0
	m.offset = 0
	m.query = ""
	m.searching = false
	m.rebuild()
}

func (m *Model) cycleTheme() {
	if m.theme == themeLight {
		m.theme = themeDark
	} else {
		m.theme = themeLight
	}
	applyTheme(m.theme)
	saveTheme(m.theme)
}

func detectedFromNames(names []string) []tool.Detected {
	out := make([]tool.Detected, 0, len(names))
	for _, n := range names {
		out = append(out, tool.Detected{Kind: n})
	}
	return out
}
