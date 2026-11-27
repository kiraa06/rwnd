package scan

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Hermes reads local, visible conversations from the active profile's SQLite
// store. Do not attach the database path to a Session: a session is not a file
// and must never go through the transcript trash operation.
func Hermes(path string) ([]Session, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath // Windows volume path
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query, err := hermesListingQuery(db)
	if err != nil {
		return nil, fmt.Errorf("read Hermes schema: %w", err)
	}
	rows, err := db.Query(query, previewTurns)
	if err != nil {
		return nil, fmt.Errorf("read Hermes session store: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		s := Session{Tool: "hermes"}
		var modified float64
		var first, preview string
		if err := rows.Scan(&s.ID, &s.Title, &s.Cwd, &s.Branch, &s.Model, &s.ParentID, &modified, &first, &preview); err != nil {
			return nil, fmt.Errorf("read Hermes session row: %w", err)
		}
		if strings.TrimSpace(s.Title) == "" {
			s.Title = hermesDisplay(first)
		}
		var turns []Turn
		if err := json.Unmarshal([]byte(preview), &turns); err != nil {
			return nil, fmt.Errorf("read Hermes session preview: %w", err)
		}
		for _, turn := range turns {
			if text := hermesDisplay(turn.Text); text != "" {
				s.Preview = append(s.Preview, Turn{Role: turn.Role, Text: text})
			}
		}
		if s.Title == "" {
			if len(turns) == 0 {
				continue
			}
			s.Title = "Untitled session"
		}
		seconds, fraction := math.Modf(modified)
		s.Modified = time.Unix(int64(seconds), int64(math.Round(fraction*1e9)))
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read Hermes sessions: %w", err)
	}
	markMissing(sessions)
	return sessions, nil
}

func hermesListingQuery(db *sql.DB) (string, error) {
	rows, err := db.Query(`SELECT 'sessions', name FROM pragma_table_info('sessions')
		UNION ALL SELECT 'messages', name FROM pragma_table_info('messages')`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return "", err
		}
		columns[table+"."+column] = true
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	column := func(table, alias, name, fallback string) string {
		if columns[table+"."+name] {
			return alias + "." + name
		}
		return fallback
	}
	session := func(name, fallback string) string {
		return "COALESCE(" + column("sessions", "s", name, "NULL") + ", " + fallback + ")"
	}
	message := func(name, fallback string) string { return column("messages", "m", name, fallback) }
	// Hermes display history keeps compaction archives, but not rewound,
	// explicitly hidden, or model-only rows.
	visible := fmt.Sprintf(`(%s = 1 OR %s = 1) AND COALESCE(%s, '') <> 'hidden' AND COALESCE(%s, 0) = 0`,
		message("active", "1"), message("compacted", "0"), message("display_kind", "NULL"),
		hermesJSONField(message("display_metadata", "NULL"), "model_only"))
	return strings.NewReplacer(
		"@title", session("title", "''"), "@cwd", session("cwd", "''"),
		"@branch", session("git_branch", "''"), "@model", session("model", "''"),
		"@parent", session("parent_session_id", "''"), "@activity", session("last_activity_at", "0"),
		"@ended", session("ended_at", "0"), "@archived", session("archived", "0"),
		"@hidden", session("hidden", "0"), "@visible", visible,
		"@delegate", hermesJSONField(column("sessions", "s", "model_config", "NULL"), "_delegate_from"),
		"@text", hermesMessageText,
	).Replace(hermesListing), nil
}

// Like Hermes's native marker lookup, treat malformed or absent JSON as unset.
// The column and key are internal SQL fragments, never database values.
func hermesJSONField(column, key string) string {
	return fmt.Sprintf(`CASE WHEN JSON_VALID(%[1]s) THEN JSON_EXTRACT(%[1]s, '$.%[2]s') END`, column, key)
}

// Use the session_id/message-id index for both bounded transcript lookups;
// tool output and system prompts are never read into the Go process.
const hermesListing = `SELECT s.id, @title, @cwd, @branch, @model, @parent,
	MAX(s.started_at, @activity, @ended,
	    COALESCE((SELECT m.timestamp FROM messages m WHERE m.session_id = s.id ORDER BY m.id DESC LIMIT 1), 0)) AS modified,
	CASE WHEN TRIM(@title) = '' THEN COALESCE((
	    SELECT @text FROM messages m
	    WHERE m.session_id = s.id AND @visible AND m.role = 'user' AND typeof(m.content) = 'text' AND m.content != ''
	    ORDER BY m.id LIMIT 1), '') ELSE '' END,
	(SELECT json_group_array(json_object('Role', role, 'Text', text) ORDER BY id) FROM (
	    SELECT m.id, m.role, @text AS text FROM messages m
	    WHERE m.session_id = s.id AND @visible AND m.role IN ('user', 'assistant') AND typeof(m.content) = 'text' AND m.content != ''
	    ORDER BY m.id DESC LIMIT ?))
	FROM sessions s
	WHERE s.source IN ('local', 'cli', 'tui', 'desktop', 'gui', 'web', 'acp', 'oneshot') AND @archived = 0 AND @hidden = 0
	  AND @delegate IS NULL
	ORDER BY modified DESC, s.id`

// Hermes stores multimodal content behind a NUL-prefixed JSON marker. Extract
// text parts before truncating, so large image payloads cannot hide the text.
const hermesJSONContent = `CAST(SUBSTR(CAST(m.content AS BLOB), 7) AS TEXT)`
const hermesMessageText = `SUBSTR(CASE
	WHEN SUBSTR(CAST(m.content AS BLOB), 1, 6) != X'006A736F6E3A' THEN m.content
	WHEN NOT JSON_VALID(` + hermesJSONContent + `) THEN ''
	WHEN JSON_TYPE(` + hermesJSONContent + `) = 'text' THEN JSON_EXTRACT(` + hermesJSONContent + `, '$')
	WHEN JSON_TYPE(` + hermesJSONContent + `) = 'array' THEN COALESCE((
	    SELECT GROUP_CONCAT(value ->> '$.text', ' ' ORDER BY key) FROM JSON_EACH(` + hermesJSONContent + `)
	    WHERE type = 'object' AND (value ->> '$.type') IN ('text', 'input_text', 'output_text')), '')
	ELSE '' END, 1, 8192)`

func hermesDisplay(text string) string {
	return truncate(strings.Join(strings.Fields(SanitizeDisplayText(text)), " "), titleMaxLen)
}
