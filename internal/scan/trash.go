package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TrashDir is where deleted transcripts are kept.
const TrashDir = ".trash-cl"

// Trash moves a session's transcript out of the store, together with the
// subagent directory that belongs to it.
//
// Transcripts are moved rather than unlinked: they hold hours of conversation,
// sometimes tens of megabytes of it, and a single keystroke should not destroy
// that irreversibly. A non-empty returned path is where the transcript now
// lives, even if moving its subagent directory failed.
func Trash(claudeDir string, s Session) (string, error) {
	fi, err := os.Lstat(s.Path)
	if err != nil {
		return "", err
	}
	dayDir := filepath.Join(claudeDir, TrashDir, time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return "", err
	}
	// Reserve a private directory for each deletion so Rename cannot replace
	// a transcript from an earlier deletion.
	dest, err := os.MkdirTemp(dayDir, "session-")
	if err != nil {
		return "", err
	}
	project := filepath.Base(filepath.Dir(s.Path))
	name := project + "__" + filepath.Base(s.Path)
	target := filepath.Join(dest, name)
	if err := os.Rename(s.Path, target); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	if fi.IsDir() {
		return target, nil
	}

	// Subagent transcripts live in a sibling directory named for the session.
	if sidecar := strings.TrimSuffix(s.Path, ".jsonl"); sidecar != s.Path {
		fi, err := os.Stat(sidecar)
		if err != nil && !os.IsNotExist(err) {
			return target, fmt.Errorf("transcript trashed, could not check for subagents: %w", err)
		}
		if err == nil && fi.IsDir() {
			sidecarTarget := strings.TrimSuffix(target, ".jsonl")
			if err := os.Rename(sidecar, sidecarTarget); err != nil {
				return target, fmt.Errorf("transcript trashed, subagents left behind: %w", err)
			}
		}
	}
	return target, nil
}
