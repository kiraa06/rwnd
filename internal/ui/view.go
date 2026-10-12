package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiraa06/rwnd/internal/group"
	"github.com/kiraa06/rwnd/internal/scan"
)

// Colours are adaptive so the picker reads correctly on light and dark
// terminals without asking which one it is on.
var (
	dim      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "246", Dark: "243"})
	faint    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "250", Dark: "239"})
	header   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "74"}).Bold(true)
	title    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "235", Dark: "252"})
	selected = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "17", Dark: "231"}).Bold(true)
	marker   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "84"}).Bold(true)
	accent   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "84"})
	warn     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	aiMark   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "62", Dark: "141"})
)

const (
	previewWidth = 42
	minPreviewAt = 104 // terminal width below which the preview is hidden
	ageColumn    = 5
	modelColumn  = 8
	pathColumn   = 24
	titleLines   = 2    // word-wrap budget for a session title
	forkMark     = " ⎇" // suffix on nested forks/clones; tree prefix stays
)

func (m Model) footerLines() int {
	return strings.Count(m.renderFooter(), "\n") + 1
}

// listHeight is how many list rows fit inside the bordered pane.
func (m Model) listHeight() int {
	// pane border (2) + pane title + column header + rule
	chrome := m.footerLines() + 5
	if h := m.height - chrome; h > 3 {
		return h
	}
	return 3
}

func (m Model) paneSizes() (listOuter, prevOuter int, showPrev bool) {
	w := max(m.width, 1)
	showPrev = m.showPreview && w >= minPreviewAt
	if !showPrev {
		return w, 0, false
	}
	prevOuter = previewWidth
	listOuter = w - prevOuter
	if listOuter < 40 {
		return w, 0, false
	}
	return listOuter, prevOuter, true
}

func (m Model) listWidth() int {
	listOuter, _, _ := m.paneSizes()
	return max(listOuter-4, 20) // border (2) + padding (2)
}

// clampOffset scrolls the window the minimum needed to keep the cursor visible.
// Offset is a row index; wrapping means a row can occupy more than one line,
// so we measure visual height rather than assuming one line per row.
func (m *Model) clampOffset() {
	h := m.listHeight()
	w := m.listWidth()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor && m.visualSpan(m.offset, m.cursor, w) > h {
		m.offset++
	}
	last := len(m.rows) - 1
	for m.offset > 0 && last >= 0 && m.visualSpan(m.offset-1, last, w) <= h {
		m.offset--
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor && m.visualSpan(m.offset, m.cursor, w) > h {
		m.offset++
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// visualSpan is the number of terminal lines rows [from, to] occupy.
func (m Model) visualSpan(from, to, width int) int {
	n := 0
	for i := from; i <= to && i < len(m.rows); i++ {
		n += m.rowVisualHeight(i, width)
	}
	return n
}

func (m Model) rowVisualHeight(i, width int) int {
	if i < 0 || i >= len(m.rows) {
		return 0
	}
	switch m.rows[i].kind {
	case rowHeader:
		if i > 0 {
			return 2
		}
		return 1
	case rowSession:
		return len(m.sessionTitleLines(i, width))
	default:
		return 1
	}
}

var paneBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "248", Dark: "238"})

// View renders the picker as bordered panes that fill the terminal.
func (m Model) View() string {
	applyTheme(m.theme)
	w, h := max(m.width, 1), max(m.height, 1)
	footer := clipLines(m.renderFooter(), w)
	fh := strings.Count(footer, "\n") + 1
	paneH := h - fh
	if paneH < 1 {
		paneH = 1
	}

	listOuter, prevOuter, showPrev := m.paneSizes()
	list := m.renderListPane(listOuter, paneH)
	var body string
	if !showPrev {
		body = list
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, m.renderPreviewPane(prevOuter, paneH))
	}
	placeOpts := []lipgloss.WhitespaceOption{}
	if m.theme == themeLight {
		placeOpts = append(placeOpts, lipgloss.WithWhitespaceBackground(canvasBg))
	}
	body = lipgloss.Place(w, paneH, lipgloss.Left, lipgloss.Top, body, placeOpts...)
	out := body + "\n" + footer
	if m.theme == themeLight {
		return canvas.Width(w).MaxWidth(w).MaxHeight(h).Render(out)
	}
	return out
}

func framePane(content string, outerW, outerH int) string {
	// lipgloss Width is the padded content box; Padding(0,1) is taken from it
	// and the border is added after. Width(outer-2) + border 2 = outerW, with
	// 1 cell of padding on each side leaving outerW-4 for the inner clipper.
	innerW := max(outerW-4, 1)
	innerH := max(outerH-2, 1)
	clipped := lipgloss.NewStyle().
		Width(innerW).MaxWidth(innerW).
		MaxHeight(innerH).
		Background(canvasBg).
		Render(padLines(content, innerW))
	return paneBorder.
		Width(max(outerW-2, 1)).MaxWidth(outerW).
		Height(innerH).MaxHeight(outerH).
		Padding(0, 1).
		Render(clipped)
}

func (m Model) renderListPane(outerW, outerH int) string {
	innerW := max(outerW-4, 20)
	var b strings.Builder
	b.WriteString(m.renderTitleBar(innerW))
	b.WriteByte('\n')
	b.WriteString(m.renderColumnHeader(innerW))
	b.WriteByte('\n')
	b.WriteString(faint.Render(strings.Repeat("─", max(innerW, 8))))
	b.WriteByte('\n')
	b.WriteString(m.renderList(innerW))
	return framePane(b.String(), outerW, outerH)
}

func (m Model) renderPreviewPane(outerW, outerH int) string {
	return framePane(m.renderPreview(max(outerW-4, 8)), outerW, outerH)
}

func (m Model) renderTitleBar(width int) string {
	left := accent.Render("cl") + dim.Render(fmt.Sprintf("  %d", len(m.sessions)))
	if m.query != "" {
		shown := 0
		for _, r := range m.rows {
			if r.kind == rowSession {
				shown++
			}
		}
		left += dim.Render(fmt.Sprintf(" · %d matching", shown))
	}
	if len(m.tools) >= 2 {
		left += dim.Render("  agent ") + accent.Render(m.currentTool())
	}
	left += dim.Render("  theme ") + accent.Render(m.themeName())
	return pad(left, width)
}

func (m Model) themeName() string {
	if m.theme == themeLight {
		return themeLight
	}
	return themeDark
}

func (m Model) renderColumnHeader(width int) string {
	tw, showPath := m.titleColWidth(width)
	title := dim.Render(pad("Title", tw))
	if showPath {
		title += faint.Render(" │ ") + dim.Render(pad("Path", pathColumn))
	}
	title += faint.Render(" │ ") + dim.Render(pad("Age", ageColumn))
	title += faint.Render(" │ ") + dim.Render(pad("Model", modelColumn))
	return pad(title, width)
}

func (m Model) renderList(width int) string {
	if len(m.rows) == 0 {
		return pad(faint.Render("  no sessions"), width)
	}
	h := m.listHeight()

	var b strings.Builder
	used := 0
	if m.offset > 0 {
		b.WriteString(pad(faint.Render(fmt.Sprintf("  ↑ %d more", m.offset)), width))
		b.WriteByte('\n')
		used++
	}
	end := m.offset
	for i := m.offset; i < len(m.rows); i++ {
		vh := m.rowVisualHeight(i, width)
		need := vh
		if i < len(m.rows)-1 {
			need++ // leave a line for ↓ N more
		}
		if used > 0 && used+need > h {
			break
		}
		b.WriteString(m.renderRow(i, width))
		b.WriteByte('\n')
		used += vh
		end = i + 1
	}
	if end < len(m.rows) {
		b.WriteString(pad(faint.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)), width))
	}
	return b.String()
}

func (m Model) renderRow(i, width int) string {
	r := m.rows[i]
	switch r.kind {
	case rowHeader:
		line := header.Render(r.text)
		if r.count > 0 {
			line += dim.Render(fmt.Sprintf("  %d", r.count))
		}
		if i > 0 {
			return "\n" + pad(line, width)
		}
		return pad(line, width)
	case rowNote:
		return pad(faint.Render("    "+r.text), width)
	case rowNew:
		return m.renderNewRow(i, width)
	default:
		return m.renderSessionRow(i, width)
	}
}

func (m Model) renderNewRow(i, width int) string {
	tw, showPath := m.titleColWidth(width)
	if i == m.cursor {
		label := marker.Render("  ▸ ") + selected.Render("New session") + dim.Render("  in this directory")
		return m.joinCols(pad(label, tw), "", "", "", true, showPath)
	}
	label := fill(4) + accent.Render("New session")
	return m.joinCols(pad(label, tw), "", "", "", false, showPath)
}

func (m Model) renderSessionRow(i, width int) string {
	r := m.rows[i]
	s := r.display
	lines := m.sessionTitleLines(i, width)
	sel := i == m.cursor

	var path string
	if r.section != group.KindCwd {
		path = group.Abbreviate(s.Cwd)
	}
	ageCell := age(s.Modified)
	if s.Missing {
		ageCell = "gone"
	}
	modelCell := shortModel(s.Model)

	cursor, tree := m.sessionPrefix(i)
	tw, showPath := m.titleColWidth(width)
	inner := max(tw-lipgloss.Width(cursor+tree), 8)
	painted := m.paintTitle(lines[0], sel, r.context)
	if s.AITitled {
		painted += aiMark.Render(" ·")
	}
	if s.ParentID != "" {
		painted += faint.Render(forkMark)
	}
	name := pad(painted, inner)
	var title string
	if sel {
		title = marker.Render(cursor) + faint.Render(tree) + name
	} else {
		title = cursor + faint.Render(tree) + name
	}

	line := m.joinCols(title, path, ageCell, modelCell, sel, showPath)
	if len(lines) == 1 {
		return line
	}
	cont := fill(lipgloss.Width(cursor+tree)) + m.paintTitle(lines[1], sel, r.context)
	return line + "\n" + m.joinCols(pad(cont, tw), "", "", "", sel, showPath)
}

func (m Model) sessionPrefix(i int) (cursor, tree string) {
	r := m.rows[i]
	if i == m.cursor {
		cursor = "  ▸ "
	} else {
		cursor = fill(4)
	}
	if r.depth <= 0 {
		return cursor, ""
	}
	var b strings.Builder
	for d := 1; d < r.depth; d++ {
		b.WriteString("   ")
	}
	if r.last {
		b.WriteString("└─ ")
	} else {
		b.WriteString("├─ ")
	}
	return cursor, b.String()
}

func (m Model) titleColWidth(width int) (titleW int, showPath bool) {
	showPath = width >= 72
	// joinCols: title + [sep+path] + sep+age + sep+model. Each sep is 3 cells.
	seps := 2
	meta := ageColumn + modelColumn
	if showPath {
		seps++
		meta += pathColumn
	}
	meta += seps * 3
	titleW = max(width-meta, 12)
	return titleW, showPath
}

func (m Model) titleWidth(i, width int) int {
	tw, _ := m.titleColWidth(width)
	cursor, tree := m.sessionPrefix(i)
	inner := max(tw-lipgloss.Width(cursor+tree), 8)
	if i < 0 || i >= len(m.rows) {
		return inner
	}
	s := m.rows[i].session
	if s.AITitled {
		inner = max(inner-2, 8)
	}
	if s.ParentID != "" {
		inner = max(inner-lipgloss.Width(forkMark), 8)
	}
	return inner
}

func (m Model) sessionTitleLines(i, width int) []string {
	return wrapTitle(m.rows[i].display.Title, m.titleWidth(i, width), titleLines)
}

func (m Model) joinCols(title, path, ageCell, modelCell string, sel, showPath bool) string {
	sep := faint.Render(" │ ")
	if sel && (path != "" || ageCell != "" || modelCell != "") {
		sep = selected.Render(" │ ")
	}
	ageOut := metaCell(ageCell, ageColumn, sel, dim)
	if ageCell == "gone" {
		ageOut = warn.Render(pad("gone", ageColumn))
	}
	modelOut := metaCell(modelCell, modelColumn, sel, dim)
	pathOut := metaCell(path, pathColumn, sel, faint)
	line := title
	if showPath {
		line += sep + pathOut
	}
	return line + sep + ageOut + sep + modelOut
}

func metaCell(text string, width int, sel bool, base lipgloss.Style) string {
	cell := pad(clip(text, width), width)
	if text == "" {
		return fill(width)
	}
	if sel {
		return selected.Render(cell)
	}
	return base.Render(cell)
}

func (m Model) parentTitle(id string) string {
	for _, r := range m.rows {
		if r.kind == rowSession && r.session.ID == id {
			if r.display.Title != "" {
				return r.display.Title
			}
			return r.display.ID
		}
	}
	return scan.SanitizeDisplayText(id)
}

// paintTitle styles a title, highlighting query terms and dimming ancestors
// that are only present so a matching fork keeps its parent.
func (m Model) paintTitle(text string, sel, context bool) string {
	base := title
	hit := accent
	if sel {
		base = selected
		hit = marker
	} else if context {
		base = faint
		hit = accent
	}
	if m.query == "" {
		return base.Render(text)
	}
	return highlight(text, strings.Fields(strings.ToLower(m.query)), base, hit)
}

func highlight(text string, terms []string, base, hit lipgloss.Style) string {
	if len(terms) == 0 || text == "" {
		return base.Render(text)
	}
	runes := []rune(text)
	fold := make([]rune, len(runes))
	for i, r := range runes {
		fold[i] = unicode.ToLower(r)
	}
	mark := make([]bool, len(runes))
	for _, t := range terms {
		if t == "" {
			continue
		}
		pat := []rune(t)
		for i, r := range pat {
			pat[i] = unicode.ToLower(r)
		}
		for i := 0; i+len(pat) <= len(fold); i++ {
			match := true
			for j, p := range pat {
				if fold[i+j] != p {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			for j := range pat {
				mark[i+j] = true
			}
			i += len(pat) - 1
		}
	}
	var b strings.Builder
	run := 0
	on := mark[0]
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || mark[i] != on {
			chunk := string(runes[run:i])
			if on {
				b.WriteString(hit.Render(chunk))
			} else {
				b.WriteString(base.Render(chunk))
			}
			if i < len(runes) {
				run, on = i, mark[i]
			}
		}
	}
	return b.String()
}

func (m Model) renderPreview(width int) string {
	if width < 8 {
		width = 8
	}
	r, ok := m.current()
	if !ok || r.kind != rowSession {
		return faint.Render("New session\n\nStarts " + m.currentTool() + " in\n" + group.Abbreviate(m.displayCwd))
	}
	s := r.display

	var b strings.Builder
	for i, line := range wrapTitle(s.Title, width, 3) {
		if i == 0 {
			b.WriteString(header.Render(line))
		} else {
			b.WriteString("\n")
			b.WriteString(header.Render(line))
		}
	}
	b.WriteString("\n")
	if s.ParentID != "" {
		label := "fork of "
		if s.Clone {
			label = "clone of "
		}
		b.WriteString(faint.Render(label + clip(m.parentTitle(r.session.ParentID), max(width-8, 8))))
		b.WriteString("\n")
	}
	if s.Missing {
		b.WriteString(warn.Render("directory no longer exists"))
		b.WriteString("\n")
	}
	b.WriteString(faint.Render(group.Abbreviate(s.Cwd)))
	if s.Branch != "" && s.Branch != "HEAD" {
		b.WriteString(faint.Render("  " + s.Branch))
	}
	b.WriteString("\n")
	b.WriteString(dim.Render(s.Modified.Format("Mon 2 Jan 15:04") + "  " + shortModel(s.Model)))
	b.WriteString("\n\n")

	if len(s.Preview) == 0 {
		b.WriteString(faint.Render("(no preview)"))
		return b.String()
	}
	for _, t := range s.Preview {
		who, style := "you", accent
		if t.Role == "assistant" {
			who, style = m.currentTool(), aiMark
		}
		b.WriteString(style.Render(who))
		b.WriteString("\n")
		b.WriteString(dim.Render(indent(wrap(clip(t.Text, 240), max(width-2, 8)), "  ")))
		b.WriteString("\n\n")
	}
	return b.String()
}

func (m Model) renderFooter() string {
	var lines []string

	if m.confirming {
		if r, ok := m.current(); ok {
			lines = append(lines, warn.Render("delete ")+
				selected.Render(clip(r.display.Title, 48))+
				warn.Render("?  y / n")+
				dim.Render("  (moved to trash, not erased)"))
		}
	} else if m.searching {
		lines = append(lines, accent.Render("search ")+selected.Render(scan.SanitizeDisplayText(m.query))+marker.Render("▌"))
	} else if m.status != "" {
		lines = append(lines, warn.Render(scan.SanitizeDisplayText(m.status)))
	}

	var models []string
	for i := range m.models {
		displayName := m.displayModels[i]
		if i == m.modelIdx {
			models = append(models, marker.Render("‹ ")+selected.Render(displayName)+marker.Render(" ›"))
			continue
		}
		models = append(models, faint.Render(displayName))
	}
	lines = append(lines, dim.Render("model  ")+strings.Join(models, fill(2)))

	hints := "↑↓ move · pgup/pgdn · ←→ model · ⏎ start · f fork · y copy id · d delete · / search · p preview · T dark/light · q quit"
	if len(m.tools) >= 2 {
		hints = "↑↓ move · pgup/pgdn · ←→ model · ⏎ start · f fork · y copy id · d delete · t " + strings.Join(m.tools, "/") + " · T dark/light · / search · p preview · q quit"
	}
	if m.searching {
		hints = "type to filter · ↑↓ move · ⏎ start · esc clear"
	}
	lines = append(lines, faint.Render(hints))
	return strings.Join(lines, "\n")
}

// age renders a compact relative time: the list is scanned, not read.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// shortModel reduces a recorded model id to something that fits a column.
// Transcripts also carry pseudo-models for internal records (compaction writes
// "<synthetic>"); those name nothing launchable, so the column stays blank.
func shortModel(id string) string {
	if id == "" || !isModelName(id) {
		return ""
	}
	s := strings.TrimPrefix(id, "claude-")
	if i := strings.Index(s, "-"); i > 0 {
		if _, err := fmt.Sscanf(s[i+1:], "%d", new(int)); err == nil {
			s = s[:i]
		}
	}
	return clip(s, modelColumn)
}

// isModelName reports whether an id looks like a real model identifier.
func isModelName(id string) bool {
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '.' &&
			r != '[' && r != ']' && r != '_' {
			return false
		}
	}
	return true
}

// clip truncates to a rune budget, marking the cut.
func clip(s string, width int) string {
	r := []rune(s)
	if width <= 1 || len(r) <= width {
		return s
	}
	return strings.TrimRight(string(r[:width-1]), " ") + "…"
}

// pad right-pads to a display width.
func fill(n int) string {
	if n <= 0 {
		return ""
	}
	return canvas.Render(strings.Repeat(" ", n))
}

func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + fill(gap)
	}
	return s
}

func padLines(s string, width int) string {
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = pad(p, width)
	}
	return strings.Join(parts, "\n")
}

// wrap breaks text into lines of at most width.
func wrap(s string, width int) string {
	if width < 8 {
		width = 8
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// wrapTitle word-wraps s to width, then keeps at most maxLines, ellipsizing
// the last line if the text still overflows.
func wrapTitle(s string, width, maxLines int) []string {
	if maxLines < 1 {
		maxLines = 1
	}
	if width < 8 {
		width = 8
	}
	raw := strings.Split(wrap(s, width), "\n")
	var lines []string
	for _, line := range raw {
		if lipgloss.Width(line) > width {
			line = clip(line, width)
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	if len(lines) <= maxLines {
		return lines
	}
	kept := append([]string{}, lines[:maxLines-1]...)
	rest := strings.Join(lines[maxLines-1:], " ")
	kept = append(kept, clip(rest, width))
	return kept
}

// indent prefixes every line, so a wrapped block stays visually one unit.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func clipLines(s string, w int) string {
	if w < 1 {
		w = 1
	}
	clipper := lipgloss.NewStyle().MaxWidth(w)
	parts := strings.Split(s, "\n")
	for i, line := range parts {
		parts[i] = clipper.Render(line)
	}
	return strings.Join(parts, "\n")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
