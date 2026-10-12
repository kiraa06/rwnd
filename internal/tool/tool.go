package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kiraa06/rwnd/internal/launch"
	"github.com/kiraa06/rwnd/internal/scan"
)

const (
	Claude = "claude"
	Grok   = "grok"
	Codex  = "codex"
)

var order = []string{Claude, Grok, Codex}

var grokFamilies = []string{"grok-4.6", "grok-4.5"}

// Detected is a coding agent that is both on PATH and launchable.
type Detected struct {
	Kind string
	Bin  string
}

// Detect returns claude, grok and/or codex, in that order, when the binary
// is on PATH. An empty session store is still launchable (New session).
func Detect() []Detected {
	var out []Detected
	for _, kind := range order {
		path, err := exec.LookPath(kind)
		if err != nil {
			continue
		}
		out = append(out, Detected{Kind: kind, Bin: path})
	}
	return out
}

// List sessions for kind under the user's home directory.
func List(kind, home string) ([]scan.Session, error) {
	switch kind {
	case Grok:
		return scan.Grok(filepath.Join(home, ".grok", "sessions"))
	case Codex:
		return scan.Codex(filepath.Join(home, ".codex", "sessions"))
	default:
		return scan.All(filepath.Join(home, ".claude", "projects"))
	}
}

// Models is the footer list for kind.
func Models(kind, home string) []string {
	switch kind {
	case Grok:
		return withDefault(tomlValue(filepath.Join(home, ".grok", "config.toml"), "default"), grokFamilies)
	case Codex:
		if m := tomlValue(filepath.Join(home, ".codex", "config.toml"), "model"); m != "" {
			return []string{m}
		}
		return nil
	default:
		list, _ := launch.Models(filepath.Join(home, ".claude", "settings.json"))
		return list
	}
}

// StoreDir is the tool's home (where trash lives).
func StoreDir(kind, home string) string {
	switch kind {
	case Grok:
		return filepath.Join(home, ".grok")
	case Codex:
		return filepath.Join(home, ".codex")
	default:
		return filepath.Join(home, ".claude")
	}
}

// Binary of kind from a Detected list, or "".
func Binary(tools []Detected, kind string) string {
	for _, t := range tools {
		if t.Kind == kind {
			return t.Bin
		}
	}
	return ""
}

// Next cycles kind through tools.
func Next(tools []Detected, kind string) string {
	if len(tools) == 0 {
		return kind
	}
	for i, t := range tools {
		if t.Kind == kind {
			return tools[(i+1)%len(tools)].Kind
		}
	}
	return tools[0].Kind
}

func configPath() (string, error) {
	if p := os.Getenv("CL_CONFIG_DIR"); p != "" {
		return filepath.Join(p, "tool"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cl", "tool"), nil
}

// LoadPreferred reads the last chosen tool, or claude.
func LoadPreferred() string {
	p, err := configPath()
	if err != nil {
		return Claude
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Claude
	}
	s := strings.TrimSpace(string(data))
	switch s {
	case Claude, Grok, Codex:
		return s
	}
	return Claude
}

// SavePreferred remembers the last chosen tool.
func SavePreferred(kind string) {
	p, err := configPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte(kind+"\n"), 0o600)
}

func withDefault(def string, families []string) []string {
	list := make([]string, 0, len(families)+1)
	if def != "" {
		list = append(list, def)
	}
	for _, f := range families {
		if !contains(list, f) {
			list = append(list, f)
		}
	}
	return list
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// tomlValue is a tiny key lookup for `key = "value"` lines. It ignores tables;
// Grok and Codex keep the keys we care about unique in their config files.
func tomlValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := key + " "
	found := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, prefix) && !strings.HasPrefix(line, key+"=") {
			continue
		}
		_, rest, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if v, ok := tomlQuoted(rest); ok {
			rest = v
		} else if i := strings.Index(rest, "#"); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		if rest != "" {
			found = rest // last match wins: [models] default sits below other tables
		}
	}
	return found
}

// tomlQuoted returns the text between the first pair of double quotes.
func tomlQuoted(s string) (string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", false
	}
	rest := s[1:]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}
