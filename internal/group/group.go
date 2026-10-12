// Package group arranges sessions into the picker's three sections: the current
// directory, the rest of the current git repository, and everywhere else.
package group

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kiraa06/rwnd/internal/scan"
)

// Kind identifies a section, in the order the picker shows them.
type Kind int

const (
	KindCwd Kind = iota
	KindRepo
	KindAll
)

// Section is one labelled block of rows.
type Section struct {
	Kind     Kind
	Path     string // the directory the section is scoped to; empty for KindAll
	Sessions []scan.Session
	Hidden   int // sessions omitted by the cap, reachable through search
}

// Label is the section's header text.
func (s Section) Label() string {
	switch s.Kind {
	case KindCwd:
		return "HERE  " + Abbreviate(s.Path)
	case KindRepo:
		if s.Path == "" {
			// The cwd is the repository root, so naming it again would just
			// repeat the section above.
			return "REPO  elsewhere in this repo"
		}
		return "REPO  " + Abbreviate(s.Path)
	default:
		return "ALL PROJECTS"
	}
}

// RepoRoot walks up from dir to the nearest git repository root, returning ""
// when dir is not in a repository. It accepts .git as a file as well as a
// directory, so linked worktrees resolve to the worktree itself.
func RepoRoot(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Build splits sessions into sections. cwd holds the first section; repoRoot,
// when non-empty, holds everything else inside the repository; the remainder
// falls to the last section, capped at allCap rows.
//
// Sections that would be empty are dropped, so a directory outside any
// repository simply shows two.
func Build(sessions []scan.Session, cwd, repoRoot string, allCap int) []Section {
	cwd = filepath.Clean(cwd)
	if repoRoot != "" {
		repoRoot = filepath.Clean(repoRoot)
	}

	var here, repo, all []scan.Session
	for _, s := range sessions {
		dir := filepath.Clean(s.Cwd)
		switch {
		case dir == cwd:
			here = append(here, s)
		case repoRoot != "" && under(dir, repoRoot):
			repo = append(repo, s)
		default:
			all = append(all, s)
		}
	}

	sections := []Section{{Kind: KindCwd, Path: cwd, Sessions: here}}
	if len(repo) > 0 {
		path := repoRoot
		if path == cwd {
			path = "" // the header would otherwise repeat the cwd section
		}
		sections = append(sections, Section{Kind: KindRepo, Path: path, Sessions: repo})
	}
	if len(all) > 0 {
		sec := Section{Kind: KindAll, Sessions: all}
		if allCap > 0 {
			sec.Sessions, sec.Hidden = CapRoots(all, allCap)
		}
		sections = append(sections, sec)
	}
	return sections
}

// under reports whether dir is inside root.
func under(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// Abbreviate shortens a path for display, replacing the home directory with ~
// and eliding the middle of a long path rather than its ends, which carry the
// most meaning.
func Abbreviate(path string) string {
	if path == "" {
		return "(unknown)"
	}
	if home, err := os.UserHomeDir(); err == nil && under(path, home) {
		path = "~" + strings.TrimPrefix(path, home)
	}
	const max = 46
	if len([]rune(path)) <= max {
		return path
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) < 4 {
		return path
	}
	head := strings.Join(parts[:2], string(filepath.Separator))
	tail := strings.Join(parts[len(parts)-2:], string(filepath.Separator))
	return fmt.Sprintf("%s/…/%s", head, tail)
}
