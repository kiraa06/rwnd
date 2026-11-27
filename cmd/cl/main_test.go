package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/scan"
	"github.com/kiraa06/rwnd/internal/tool"
	"github.com/kiraa06/rwnd/internal/ui"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args  []string
		query string
		act   action
	}{
		{nil, "", actionPick},
		{[]string{"kafka"}, "kafka", actionPick},
		{[]string{"kafka", "consumer"}, "kafka consumer", actionPick},
		{[]string{"--help"}, "", actionHelp},
		{[]string{"-v"}, "", actionVersion},
		{[]string{"--grok", "heap"}, "heap", actionPick},
	}
	for _, tc := range tests {
		q, a, _ := parseArgs(tc.args)
		if q != tc.query || a != tc.act {
			t.Errorf("parseArgs(%q) = %q %v, want %q %v", tc.args, q, a, tc.query, tc.act)
		}
	}
	_, _, k := parseArgs([]string{"--codex"})
	if k != "codex" {
		t.Errorf("tool = %q, want codex", k)
	}
}

func TestParseArgsHermes(t *testing.T) {
	query, action, kind := parseArgs([]string{"--hermes", "release notes"})
	if query != "release notes" || action != actionPick || kind != "hermes" {
		t.Fatalf("got query=%q action=%v kind=%q", query, action, kind)
	}
}

func TestHermesHandoffPinsDisplayedStore(t *testing.T) {
	launcherDir, projectDir := t.TempDir(), t.TempDir()
	t.Chdir(launcherDir)
	launcherDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		label, store string
		mode         launch.Mode
		args         []string
	}{
		{"default resume", "custom root", launch.Resume, []string{"--resume", "session-id", "--profile", "default"}},
		{"default new", "custom root", launch.New, []string{"--profile", "default"}},
		{"named resume", filepath.Join("custom root", "profiles", "research"), launch.Resume, []string{"--resume", "session-id"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("HERMES_HOME", tc.store)
			if tc.label == "named resume" {
				t.Setenv("HERMES_HOME", "different from displayed store")
			}
			choice := &ui.Choice{Tool: tool.Hermes, StoreDir: tc.store, Mode: tc.mode,
				Session: scan.Session{ID: "session-id", Cwd: projectDir}, Model: "never override Hermes routing"}
			spec, err := prepareLaunch(tool.Hermes, "hermes", launcherDir, choice)
			if err != nil {
				t.Fatal(err)
			}
			if want := append([]string{"hermes"}, tc.args...); !slices.Equal(spec.Args, want) {
				t.Errorf("argv = %q, want %q", spec.Args, want)
			}
			if got, want := os.Getenv("HERMES_HOME"), filepath.Join(launcherDir, tc.store); got != want {
				t.Errorf("HERMES_HOME = %q, want %q", got, want)
			}
			wantDir := launcherDir
			if tc.mode == launch.Resume {
				wantDir = projectDir
			}
			if spec.Dir != wantDir {
				t.Errorf("cwd = %q, want %q", spec.Dir, wantDir)
			}
		})
	}
}
