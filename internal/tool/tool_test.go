package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNextCycles(t *testing.T) {
	tools := []Detected{{Kind: Claude}, {Kind: Grok}, {Kind: Codex}}
	if got := Next(tools, Claude); got != Grok {
		t.Errorf("next claude = %q, want grok", got)
	}
	if got := Next(tools, Codex); got != Claude {
		t.Errorf("next codex = %q, want claude", got)
	}
}

func TestTomlValueLastMatchWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "name = \"other\"\n[models]\ndefault = \"grok-4.6\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tomlValue(path, "default"); got != "grok-4.6" {
		t.Errorf("default = %q", got)
	}
}

func TestTomlValueStripsTrailingComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "model = \"gpt-5.1-codex\"  # the default\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tomlValue(path, "model"); got != "gpt-5.1-codex" {
		t.Errorf("model = %q, want gpt-5.1-codex", got)
	}
}

func TestLoadPreferred(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_CONFIG_DIR", dir)
	if got := LoadPreferred(); got != Claude {
		t.Errorf("empty config = %q, want claude", got)
	}
	SavePreferred(Grok)
	if got := LoadPreferred(); got != Grok {
		t.Errorf("saved = %q, want grok", got)
	}
}

func TestHermesUsesActiveHomeAndCanBeRemembered(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "active profile")
	t.Setenv("HERMES_HOME", profile)
	t.Setenv("CL_CONFIG_DIR", t.TempDir())
	if got := StoreDir("hermes", home); got != profile {
		t.Errorf("store = %q, want %q", got, profile)
	}
	SavePreferred("hermes")
	if got := LoadPreferred(); got != "hermes" {
		t.Errorf("preferred = %q, want hermes", got)
	}
	if got := Models("hermes", home); len(got) != 0 {
		t.Errorf("Hermes should manage its own model and provider, got %v", got)
	}
}

func TestHermesExpandsActiveHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("RWND_TEST_PROFILE", "research profile")
	t.Setenv("HERMES_HOME", "~/.hermes/profiles/$RWND_TEST_PROFILE")
	if got, want := StoreDir("hermes", home), filepath.Join(home, ".hermes", "profiles", "research profile"); got != want {
		t.Errorf("store = %q, want %q", got, want)
	}
}

func TestHermesFollowsStickyProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "Local"))
	t.Setenv("HERMES_DATA_DIR_SUFFIX", "")
	root := StoreDir(Hermes, home)
	profile := filepath.Join(root, "profiles", "research")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "active_profile"), []byte("\ufeffresearch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", root} {
		t.Setenv("HERMES_HOME", override)
		if got := StoreDir(Hermes, home); got != profile {
			t.Errorf("with HERMES_HOME=%q: store = %q, want %q", override, got, profile)
		}
	}
}

func TestHermesSeparatesStickyMarkerFromLaunchRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("HERMES_DATA_DIR_SUFFIX", "-picker-test")
	native := filepath.Join(home, ".hermes-picker-test")
	if runtime.GOOS == "windows" {
		native = filepath.Join(home, "AppData", "Local", "hermes-picker-test")
	}
	writeMarker := func(root, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "profiles", name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "active_profile"), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(native, "research")
	nested := filepath.Join(native, "work")
	writeMarker(nested, "local")
	if err := os.MkdirAll(filepath.Join(nested, "profiles", "research"), 0o700); err != nil {
		t.Fatal(err)
	}
	custom := native + "-custom"
	writeMarker(custom, "local")
	for _, tc := range []struct{ label, override, want string }{
		{"nested launch root", nested, filepath.Join(nested, "profiles", "research")},
		{"separate custom root", custom, filepath.Join(custom, "profiles", "local")},
		{"explicit named profile", filepath.Join(nested, "profiles", "local"), filepath.Join(nested, "profiles", "local")},
	} {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("HERMES_HOME", tc.override)
			if got := StoreDir(Hermes, home); got != tc.want {
				t.Errorf("store = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("alias into native root", func(t *testing.T) {
		alias := filepath.Join(home, "alias")
		if err := os.Symlink(nested, alias); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		t.Setenv("HERMES_HOME", alias)
		if got, want := StoreDir(Hermes, home), filepath.Join(alias, "profiles", "research"); got != want {
			t.Errorf("store = %q, want %q", got, want)
		}
	})
}

func TestCodexUsesCustomHomeForSessionsAndModel(t *testing.T) {
	home, profile := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", profile)
	if err := os.Mkdir(filepath.Join(profile, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.toml"), []byte("model = \"profile-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollout := `{"type":"session_meta","payload":{"id":"profile-session","cwd":"/workspace"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":"Read the active Codex profile"}}
`
	if err := os.WriteFile(filepath.Join(profile, "sessions", "rollout-test.jsonl"), []byte(rollout), 0o600); err != nil {
		t.Fatal(err)
	}
	sessions, err := ListStore(Codex, StoreDir(Codex, home))
	if err != nil || len(sessions) != 1 || sessions[0].ID != "profile-session" {
		t.Errorf("sessions = %v, error = %v", sessions, err)
	}
	if got := Models(Codex, home); len(got) != 1 || got[0] != "profile-model" {
		t.Errorf("models = %v, want profile-model", got)
	}
	if got := StoreDir(Codex, home); got != profile {
		t.Errorf("store = %q, want %q", got, profile)
	}
}
