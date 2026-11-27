package scan

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Keep notification names first: they are the only wrappers stripped from
// assistant replies, where other tags may accompany ordinary explanation.
var codexContextNames = [...]string{
	"task-notification", "subagent_notification",
	"environment_context", "user_instructions", "permissions instructions",
	"external_codex_apps_open_page", "external_codex_apps_context",
	"workspace_context", "turn_aborted", "skills_instructions", "skill",
	"recommended_plugins", "user_shell_command",
}

const codexNotificationKinds = 2
const codexPluginNotice = "Here is a list of plugins that are available but not installed"

func codexCleanBlock(raw string, user bool) string {
	text := strings.TrimSpace(raw)
	if codexBoilerplate(text, user) {
		return ""
	}
	if user {
		text = codexWithoutAgents(text)
	}
	text = strings.TrimSpace(codexWithoutContext(text, user))
	if user {
		text = codexWithoutAgents(text)
	}
	if codexBoilerplate(text, user) {
		return ""
	}
	return text
}

func codexBoilerplate(text string, user bool) bool {
	return (user && strings.HasPrefix(text, codexPluginNotice)) || codexNotification(text)
}

type codexContextOpening struct {
	start, body, close, end int // byte offsets in the original block
	next                    int // prior opener until paired; first opener after the close once paired
	kind                    uint8
	lineBlank, machine      bool
}

// Index pairs once in source order, using a stack per kind for nested wrappers.
// An unmatched opener does not require searching the remaining text again.
func codexContextOpenings(text string, user bool) []codexContextOpening {
	if !strings.Contains(text, "<") {
		return nil
	}
	var openings []codexContextOpening
	var stack [len(codexContextNames)]int // one-based opener indices; zero means empty
	names := codexContextNames[:codexNotificationKinds]
	if user {
		names = codexContextNames[:]
	}
	// Track the retained line boundary after complete outer wrappers, visiting
	// each inter-tag gap at most once. Inside an open wrapper, use raw line
	// boundaries: it might be an inline example that cleanup preserves whole.
	var tagEnd, depth, outer int
	lineBlank := true
	codexOutsideFences(text, func() bool {
		// A fence inside injected content cannot hide the envelope's own
		// closing tag. Only start tracking fences in retained prompt text.
		for _, top := range stack {
			if top == 0 {
				continue
			}
			o := openings[top-1]
			if o.lineBlank || o.machine {
				return false
			}
		}
		return true
	}, func(start, end int) {
		for pos := start; pos < end; {
			off := strings.IndexByte(text[pos:end], '<')
			if off < 0 {
				break
			}
			start := pos + off
			delim := strings.IndexAny(text[start+1:end], "<>")
			if delim < 0 {
				break
			}
			pos = start + 1 + delim
			if text[pos] == '<' {
				continue
			}
			tag := text[start+1 : pos]
			pos++
			name, closing := strings.CutPrefix(tag, "/")
			for kind, want := range names {
				rest, ok := strings.CutPrefix(name, want)
				if !ok || (rest != "" && (closing || !strings.ContainsRune(" \t\r\n\f", rune(rest[0])))) {
					continue
				}
				if !closing {
					blank := codexLineBlank(depth == 0 && lineBlank, text[tagEnd:start])
					openings = append(openings, codexContextOpening{start: start, body: pos, next: stack[kind], kind: uint8(kind),
						lineBlank: blank, machine: user && codexContextBody(text[pos:])})
					stack[kind] = len(openings)
					if depth == 0 {
						outer = len(openings)
					}
					depth++
					tagEnd = pos
				} else if top := stack[kind]; top != 0 {
					o := &openings[top-1]
					stack[kind] = o.next
					o.close, o.end, o.next = start, pos, len(openings)
					depth--
					// Removal inserts whitespace at the opener's boundary; a
					// retained tag (or a crossing close) ends in nonblank '>'.
					lineBlank, tagEnd = depth == 0 && top == outer && o.lineBlank, pos
				}
				break
			}
		}
	})
	return openings
}

func codexWithoutContext(text string, user bool) string {
	openings := codexContextOpenings(text, user)
	var out strings.Builder
	copied, pos := 0, 0
	lineBlank := true
	for i := 0; i < len(openings); {
		o := openings[i]
		lineBlank = codexLineBlank(lineBlank, text[pos:o.start])
		body := text[o.body:]
		machine := o.machine
		if o.end != 0 && o.kind < codexNotificationKinds && !lineBlank && !machine {
			machine = codexNotification(strings.TrimSpace(text[o.body:o.close]))
		}
		switch {
		case o.end != 0 && (lineBlank || machine):
			out.WriteString(text[copied:o.start])
			out.WriteByte(' ')
			copied, pos, i = o.end, o.end, o.next
		case o.end == 0 && lineBlank && (machine || strings.HasPrefix(body, "\n") || strings.HasPrefix(body, "\r\n")):
			out.WriteString(text[copied:o.start])
			return out.String()
		case o.end != 0: // preserve the whole inline example, including nested text
			lineBlank = codexLineBlank(lineBlank, text[o.start:o.end])
			pos, i = o.end, o.next
		default:
			lineBlank = false // the unremoved opening tag ends in '>'
			pos, i = o.body, i+1
		}
	}
	if copied == 0 {
		return text
	}
	out.WriteString(text[copied:])
	return out.String()
}

func codexLineBlank(blank bool, span string) bool {
	if newline := strings.LastIndexByte(span, '\n'); newline >= 0 {
		return strings.TrimSpace(span[newline+1:]) == ""
	}
	return blank && strings.TrimSpace(span) == ""
}

// Visit spans outside top-level Markdown backtick or tilde fences. Opening
// fences may specify a language; closing fences need at least as many markers.
func codexOutsideFences(text string, allowFence func() bool, visit func(start, end int)) {
	if !strings.ContainsAny(text, "`~") {
		visit(0, len(text))
		return
	}
	var marker byte
	var width, start, pos int
	for pos < len(text) {
		line, rest, _ := strings.Cut(text[pos:], "\n")
		next := len(text) - len(rest)
		indented := strings.TrimLeft(line, " ")
		if len(indented) > 0 && len(line)-len(indented) <= 3 && (indented[0] == '`' || indented[0] == '~') {
			mark := indented[0]
			count := 1
			for count < len(indented) && indented[count] == mark {
				count++
			}
			tail := indented[count:]
			if width == 0 && count >= 3 && (mark != '`' || !strings.Contains(tail, "`")) {
				visit(start, pos)
				start = pos
				if allowFence == nil || allowFence() {
					marker, width = mark, count
				}
			} else if mark == marker && width != 0 && count >= width && strings.TrimSpace(tail) == "" {
				width, start = 0, next
			}
		}
		pos = next
	}
	if width == 0 {
		visit(start, len(text))
	}
}

func codexStripDisplayTags(text string) string {
	if !strings.Contains(text, "<") {
		return text
	}
	var out strings.Builder
	var copied int
	codexOutsideFences(text, nil, func(start, end int) {
		span := text[start:end]
		if !strings.Contains(span, "<") {
			return
		}
		out.WriteString(text[copied:start])
		out.WriteString(loneTag.ReplaceAllString(span, " "))
		copied = end
	})
	if copied == 0 {
		return text
	}
	out.WriteString(text[copied:])
	return out.String()
}

func codexWithoutAgents(text string) string {
	header, body, multiline := strings.Cut(text, "\n")
	header = strings.TrimSpace(header)
	if header != "# AGENTS.md instructions" && header != "AGENTS.md instructions" &&
		!(multiline && strings.HasPrefix(header, "# AGENTS.md instructions for ")) {
		return text
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "<INSTRUCTIONS>") {
		if end := strings.Index(body, "</INSTRUCTIONS>"); end >= 0 {
			return strings.TrimSpace(body[end+len("</INSTRUCTIONS>"):])
		}
		return ""
	}
	// A blank paragraph may be another instruction. Only an explicit request
	// separator establishes where an unwrapped manual ends.
	for _, separator := range []string{"\n## User request\n", "\n<user_request>"} {
		if _, request, ok := strings.Cut(body, separator); ok {
			return strings.TrimSpace(strings.TrimSuffix(request, "</user_request>"))
		}
	}
	return ""
}

func codexContextBody(body string) bool {
	body = strings.TrimLeftFunc(body, unicode.IsSpace)
	for _, prefix := range []string{
		"Current directory:", "Current working directory:", "<cwd>", "<shell>",
		"<current_date>", "<permissions", "Exit code:", "<command>",
		codexPluginNotice,
	} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	return false
}

func codexNotification(text string) bool {
	if !strings.HasPrefix(text, "{") {
		return false
	}
	var notice struct {
		AgentPath string                     `json:"agent_path"`
		Status    map[string]json.RawMessage `json:"status"`
	}
	return json.Unmarshal([]byte(text), &notice) == nil && notice.AgentPath != "" && len(notice.Status) != 0
}

func codexTitleGeneration(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "Generate metadata for a coding agent based on the user prompt.") && strings.Contains(text, "Title:")
}
