// Package scan discovers Claude Code sessions and extracts display metadata
// from their JSONL transcripts.
//
// Transcripts routinely reach tens of megabytes, so a session is never read in
// full. Only a head and tail window are read, and within those windows a byte
// prefilter keeps json.Unmarshal away from every line that cannot contribute.
package scan

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// headWindow covers the first human prompt in 113 of 120 real sessions;
	// widening it to 1MB gains only two more, so stragglers use deepScan.
	headWindow = 64 << 10
	// tailWindow always contains the newest ai-title record.
	tailWindow = 64 << 10
	// wholeFileLimit reads small transcripts in one pass instead of two.
	wholeFileLimit = 2 * (headWindow + tailWindow)
	// deepScanLimit bounds the fallback search for a late first prompt.
	deepScanLimit = 2 << 20
	previewTurns  = 6
	titleMaxLen   = 96
	// minTitleLen is the shortest prompt considered descriptive enough to title
	// a session; below it, bestTitle looks at the next prompt.
	minTitleLen = 18
	// titleCandidates bounds how many opening prompts bestTitle considers.
	titleCandidates = 5
)

// Turn is one side of an exchange, kept for the preview pane.
type Turn struct {
	Role string // "user" or "assistant"
	Text string
}

// Session is everything the picker needs to show and launch one session.
type Session struct {
	ID          string
	Path        string
	Title       string
	AITitled    bool // title came from Claude's own ai-title record
	Cwd         string
	Branch      string
	Model       string
	Modified    time.Time
	Preview     []Turn
	ParentID    string // session this one was forked or cloned from, if any
	ContinuedIn string // session this one was continued in, if any
	Clone       bool   // Claude marked this as a branch/clone
	Missing     bool   // working directory no longer exists
	Tool        string // owning backend; empty means Claude
}

// Discover returns the transcript paths of real sessions under root.
//
// Real sessions live at exactly <root>/<munged-cwd>/<uuid>.jsonl. Deeper paths
// hold subagent sidechains (112 of the 261 files on a typical disk) and the
// auto-memory directory; both are excluded.
func Discover(root string) ([]string, error) {
	projects, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, p.Name()))
		if err != nil {
			continue // an unreadable project dir should not sink the listing
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			paths = append(paths, filepath.Join(root, p.Name(), e.Name()))
		}
	}
	return paths, nil
}

// All scans every session under root concurrently, newest first. Sessions with
// no human turn are omitted: there is nothing in them worth resuming.
func All(root string) ([]Session, error) {
	paths, err := Discover(root)
	if err != nil {
		return nil, err
	}

	out := make([]Session, len(paths))
	var wg sync.WaitGroup
	work := make(chan int)

	workers := runtime.NumCPU() * 2 // I/O bound, so oversubscribe
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				if s, ok := one(paths[i]); ok {
					out[i] = s
				}
			}
		}()
	}
	for i := range paths {
		work <- i
	}
	close(work)
	wg.Wait()

	backfillCwd(out)

	sessions := make([]Session, 0, len(out))
	for _, s := range out {
		// An empty cwd means resume cannot chdir into the session's project
		// directory, so the session is not launchable and is left out.
		if s.ID != "" && s.Cwd != "" {
			sessions = append(sessions, s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	linkContinues(sessions)
	linkClones(sessions)
	markMissing(sessions)
	return sessions, nil
}

// backfillCwd fills in a missing working directory from a sibling session.
// Every transcript in a project directory was recorded from the same cwd, so a
// stub session whose own records never mention it can borrow one, rather than
// trying to reverse the directory's mangled name.
func backfillCwd(sessions []Session) {
	byProject := make(map[string]string)
	for _, s := range sessions {
		if s.ID != "" && s.Cwd != "" {
			byProject[filepath.Dir(s.Path)] = s.Cwd
		}
	}
	for i := range sessions {
		if sessions[i].ID != "" && sessions[i].Cwd == "" {
			sessions[i].Cwd = byProject[filepath.Dir(sessions[i].Path)]
		}
	}
}

func one(path string) (Session, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return Session{}, false
	}
	head, tail, err := windows(path, fi.Size())
	if err != nil {
		return Session{}, false
	}

	m := Parse(head, tail)

	// Without an ai-title the row leans on the prompt, so a missing or terse
	// one is worth a second, deeper look: sessions that open with "ok" or
	// "Yoo!" carry the real request a turn or two later, often just past the
	// head window.
	if m.AITitle == "" && m.CustomTitle == "" && len([]rune(m.Title)) < minTitleLen {
		if t := deepScan(path); len([]rune(t)) > len([]rune(m.Title)) {
			m.Title = t
		}
	}
	// Title-only stubs (ai-title / agent-name, no human prompt) are not
	// resumable; Claude writes them as branch placeholders. A custom title
	// counts: the user named the session, so they mean to resume it.
	if m.Title == "" && m.CustomTitle == "" {
		return Session{}, false
	}

	cwd := m.Cwd
	if m.RelocatedCwd != "" {
		cwd = m.RelocatedCwd
	}
	title, titled := displayTitle(m)
	s := Session{
		ID:          strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path:        path,
		Title:       title,
		AITitled:    titled,
		Cwd:         cwd,
		Branch:      m.Branch,
		Model:       m.Model,
		Modified:    fi.ModTime(),
		Preview:     m.Preview,
		ParentID:    m.ForkedFrom,
		ContinuedIn: m.ContinuedIn,
		Clone:       hasCloneMark(m.AITitle) || hasCloneMark(m.AgentName),
		Tool:        "claude",
	}
	return s, true
}

func displayTitle(m Meta) (string, bool) {
	if t := strings.TrimSpace(m.CustomTitle); t != "" {
		// User-renamed; the · marker is for titles Claude wrote itself.
		return truncate(t, titleMaxLen), false
	}
	if t := stripCloneMark(m.AITitle); t != "" {
		return t, true
	}
	if t := stripCloneMark(m.AgentName); t != "" {
		return t, true
	}
	return m.Title, false
}

// linkContinues nests a continuation under the session that pointed at it,
// when the continuation has no fork parent of its own.
func linkContinues(sessions []Session) {
	byID := make(map[string]int, len(sessions))
	for i, s := range sessions {
		if s.ID != "" {
			byID[s.ID] = i
		}
	}
	for i, s := range sessions {
		if s.ContinuedIn == "" {
			continue
		}
		j, ok := byID[s.ContinuedIn]
		if !ok || sessions[j].ParentID != "" {
			continue
		}
		sessions[j].ParentID = sessions[i].ID
	}
}

// linkClones nests Claude's ⑂-marked branches under the unmarked session
// that shares their title in the same project directory. coincidental
// identical titles without a clone mark are left as peers.
func linkClones(sessions []Session) {
	type key struct{ dir, title string }
	groups := make(map[key][]int)
	for i, s := range sessions {
		k := key{filepath.Dir(s.Path), strings.ToLower(stripCloneMark(s.Title))}
		if k.title == "" {
			continue
		}
		groups[k] = append(groups[k], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		hasClone := false
		parent := -1
		for _, i := range idxs {
			if sessions[i].Clone {
				hasClone = true
			} else if parent < 0 {
				parent = i
			}
		}
		if !hasClone {
			continue
		}
		if parent < 0 {
			parent = idxs[len(idxs)-1]
		}
		pid := sessions[parent].ID
		for _, i := range idxs {
			if i == parent || sessions[i].ParentID != "" {
				continue
			}
			sessions[i].ParentID = pid
		}
	}
}

func markMissing(sessions []Session) {
	exists := make(map[string]bool)
	for i, s := range sessions {
		if s.Cwd == "" {
			continue
		}
		ok, known := exists[s.Cwd]
		if !known {
			fi, err := os.Stat(s.Cwd)
			ok = err == nil && fi.IsDir()
			exists[s.Cwd] = ok
		}
		sessions[i].Missing = !ok
	}
}

// windows reads the head and tail slices of a transcript. Small files are read
// whole, in which case tail is nil and head holds everything.
func windows(path string, size int64) (head, tail []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	if size <= wholeFileLimit {
		head, err = io.ReadAll(f)
		return head, nil, err
	}

	head = make([]byte, headWindow)
	if _, err = io.ReadFull(f, head); err != nil {
		return nil, nil, err
	}
	head = dropPartialTail(head)

	tail = make([]byte, tailWindow)
	if _, err = f.ReadAt(tail, size-tailWindow); err != nil && err != io.EOF {
		return nil, nil, err
	}
	tail = dropPartialHead(tail)
	return head, tail, nil
}

// dropPartialTail removes a trailing record the window cut in half.
func dropPartialTail(b []byte) []byte {
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[:i+1]
	}
	return nil
}

// dropPartialHead removes a leading record the window cut in half.
func dropPartialHead(b []byte) []byte {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	return nil
}
