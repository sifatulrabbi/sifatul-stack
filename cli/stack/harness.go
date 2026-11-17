package stack

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type Harness struct {
	Key          string
	Label        string
	Binary       string
	dir          func(Layout) string
	instructions string
	agentExt     string
	skills       skillsGroup
	toggles      bool
	render       func(d agentDef, m model, enabled bool) string
}

type skillsGroup struct {
	key   string
	label string
	dir   func(Layout) string
}

var (
	claudeSkills = skillsGroup{"claude", "Claude Code", func(l Layout) string { return filepath.Join(l.Claude, "skills") }}
	sharedSkills = skillsGroup{"agents", "~/.agents", func(l Layout) string { return l.AgentsSkills }}
)

var table = []Harness{
	{Key: "claude", Label: "Claude Code", Binary: "claude", dir: func(l Layout) string { return l.Claude }, instructions: "CLAUDE.md", agentExt: ".md", skills: claudeSkills, render: renderClaude},
	{Key: "codex", Label: "Codex", Binary: "codex", dir: func(l Layout) string { return l.Codex }, instructions: "AGENTS.md", agentExt: ".toml", skills: sharedSkills, render: renderCodex},
	{Key: "opencode", Label: "OpenCode", Binary: "opencode", dir: func(l Layout) string { return l.OpenCode }, instructions: "AGENTS.md", agentExt: ".md", skills: sharedSkills, toggles: true, render: renderOpenCode},
}

var Harnesses = keys(table)

func keys(hs []Harness) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Key
	}
	return out
}

func All() []Harness { return slices.Clone(table) }

func Find(key string) (Harness, error) {
	i := slices.IndexFunc(table, func(h Harness) bool { return h.Key == key })
	if i < 0 {
		return Harness{}, fmt.Errorf("unknown harness %q; choose from %s", key, strings.Join(keys(table), ", "))
	}
	return table[i], nil
}

func (h Harness) Dir(l Layout) string { return h.dir(l) }

func (h Harness) instructionsPath(l Layout) string {
	return filepath.Join(h.dir(l), h.instructions)
}

func (h Harness) agentDir(l Layout) string { return filepath.Join(h.dir(l), "agents") }

func (h Harness) agentPath(l Layout, tier string) string {
	return filepath.Join(h.agentDir(l), tier+h.agentExt)
}

func HarnessLabel(key string) string {
	if h, err := Find(key); err == nil {
		return h.Label
	}
	for _, g := range []skillsGroup{claudeSkills, sharedSkills} {
		if g.key == key {
			return g.label
		}
	}
	return key
}

func Chosen(selected []string) []string {
	if len(selected) == 0 {
		return keys(table)
	}
	return slices.Clone(selected)
}

func ParseHarnesses(names []string) ([]string, error) {
	var want []string
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		if _, err := Find(n); err != nil {
			return nil, err
		}
		want = append(want, n)
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("choose at least one harness from %s", strings.Join(keys(table), ", "))
	}
	return slices.DeleteFunc(keys(table), func(h string) bool { return !slices.Contains(want, h) }), nil
}
