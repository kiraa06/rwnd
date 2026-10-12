// Command cl picks a Claude Code session to resume, or starts a new one.
//
// It lists sessions in three sections — the current directory first, then the
// rest of the current git repository, then everywhere else — with the title
// Claude gave each conversation, and hands the choice to claude by replacing
// itself with it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/tool"
	"github.com/kiraa06/rwnd/internal/ui"
)

// version is stamped by the release pipeline. When it is not — someone ran
// `go install`, or built from a checkout — the module version Go embeds in the
// binary is the honest answer.
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

const usage = `cl — pick a Claude Code session

usage:
  cl [query]      pick a session; a query opens the picker filtered
  cl --version    print the version
  cl --help       print this help
  cl --claude|--grok|--codex
                  force which agent the picker talks to (otherwise last used)

keys:
  ↑↓ / jk   move            ⏎   start or resume
  pgup/pgdn page            f   fork into a new session
  ←→ / hl   choose model    d   delete (moves to trash)
  /         search          y   copy session id
  p         preview         t   cycle claude / grok / codex
  T         theme dark/light
  q         quit
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cl:", err)
		os.Exit(1)
	}
}

func run() error {
	query, action, toolFlag := parseArgs(os.Args[1:])
	switch action {
	case actionHelp:
		fmt.Print(usage)
		return nil
	case actionVersion:
		fmt.Printf("cl %s\n", buildVersion())
		return nil
	}

	detected := tool.Detect()
	if len(detected) == 0 {
		return fmt.Errorf("claude, grok, or codex not found on PATH")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cwd, err := workingDir()
	if err != nil {
		return err
	}

	kind := tool.LoadPreferred()
	if env := os.Getenv("CL_TOOL"); env != "" {
		kind = env
	}
	if toolFlag != "" {
		kind = toolFlag
	}
	if tool.Binary(detected, kind) == "" {
		kind = detected[0].Kind
	}

	sessions, err := tool.List(kind, home)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	names := make([]string, len(detected))
	for i, d := range detected {
		names[i] = d.Kind
	}
	model := ui.New(sessions, cwd, filepath.Join(home, ".claude"), tool.Models(kind, home))
	model.AttachTools(home, kind, names)
	if query != "" {
		model.SetQuery(query)
	}

	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}

	choice := final.(ui.Model).Choice
	if choice == nil {
		return nil // the user quit
	}
	kind = choice.Tool
	if kind == "" {
		kind = tool.Claude
	}
	bin := tool.Binary(detected, kind)
	if bin == "" {
		return fmt.Errorf("%s not found on PATH", kind)
	}
	spec := launch.BuildFor(kind, bin, choice.Mode, choice.Session.ID, choice.Session.Cwd, cwd, choice.Model)
	return launch.Exec(spec)
}

// workingDir returns the current directory with symlinks resolved.
//
// Claude records the resolved path, so an unresolved one would file the
// sessions belonging to this directory under "all projects" instead. On macOS
// this is the difference between /tmp and /private/tmp.
func workingDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		return resolved, nil
	}
	return cwd, nil
}

// action is what the command line asked for.
type action int

const (
	actionPick action = iota
	actionHelp
	actionVersion
)

// parseArgs interprets the command line. Anything that is not a recognised
// flag is treated as a search query.
func parseArgs(args []string) (query string, a action, toolKind string) {
	var terms []string
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "help":
			return "", actionHelp, ""
		case "-v", "--version", "version":
			return "", actionVersion, ""
		case "--claude":
			toolKind = tool.Claude
		case "--grok":
			toolKind = tool.Grok
		case "--codex":
			toolKind = tool.Codex
		default:
			terms = append(terms, arg)
		}
	}
	if len(terms) == 0 {
		return "", actionPick, toolKind
	}
	return strings.Join(terms, " "), actionPick, toolKind
}
