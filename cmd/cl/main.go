// Command cl picks a local agent session to resume, or starts a new one.
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

const usage = `cl — pick a local agent session

usage:
  cl [query]      pick a session; a query opens the picker filtered
  cl --version    print the version
  cl --help       print this help
  cl --claude|--grok|--codex|--hermes
                  force which agent the picker talks to (otherwise last used)

keys:
  ↑↓ / jk   move            ⏎   start or resume
  pgup/pgdn page            f   fork (where supported)
  ←→ / hl   choose model    d   move to trash (file-based agents)
  /         search          y   copy session id
  p         preview         t   cycle installed agents
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
		return fmt.Errorf("claude, grok, codex, or hermes not found on PATH")
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
		if tool.Binary(detected, toolFlag) == "" {
			return fmt.Errorf("%s not found on PATH", toolFlag)
		}
		kind = toolFlag
	}
	if tool.Binary(detected, kind) == "" {
		kind = detected[0].Kind
	}

	names := make([]string, len(detected))
	for i, d := range detected {
		names[i] = d.Kind
	}
	model := ui.New(nil, cwd, filepath.Join(home, ".claude"), nil)
	model.AttachTools(home, kind, names)
	model.LoadSessions()
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
	spec, err := prepareLaunch(kind, bin, cwd, choice)
	if err != nil {
		return err
	}
	return launch.Exec(spec)
}

func prepareLaunch(kind, bin, cwd string, choice *ui.Choice) (launch.Spec, error) {
	spec := launch.BuildFor(kind, bin, choice.Mode, choice.Session.ID, choice.Session.Cwd, cwd, choice.Model)
	if len(spec.Args) == 0 {
		return spec, nil
	}
	// The handoff changes directory. Keep a relative profile override pointed
	// at the same store the picker just read, not at a path in the session cwd.
	var profileEnv string
	switch kind {
	case tool.Codex:
		if os.Getenv("CODEX_HOME") != "" {
			profileEnv = "CODEX_HOME"
		}
	case tool.Hermes:
		profileEnv = "HERMES_HOME"
	}
	if profileEnv != "" {
		absolute, err := filepath.Abs(choice.StoreDir)
		if err != nil {
			return launch.Spec{}, err
		}
		if kind == tool.Hermes && filepath.Base(filepath.Dir(absolute)) != "profiles" {
			// Hermes trusts a profile-shaped HERMES_HOME. For a default store,
			// explicitly bypass the sticky marker, even if it hasn't changed yet.
			spec.Args = append(spec.Args, "--profile", "default")
		}
		if err := os.Setenv(profileEnv, absolute); err != nil {
			return launch.Spec{}, err
		}
	}
	return spec, nil
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
		case "--hermes":
			toolKind = tool.Hermes
		default:
			terms = append(terms, arg)
		}
	}
	if len(terms) == 0 {
		return "", actionPick, toolKind
	}
	return strings.Join(terms, " "), actionPick, toolKind
}
