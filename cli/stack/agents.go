package stack

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var Harnesses = []string{"claude", "codex", "opencode"}

var harnessLabels = map[string]string{"claude": "Claude Code", "codex": "Codex", "opencode": "OpenCode"}

func HarnessLabel(harness string) string {
	if l, ok := harnessLabels[harness]; ok {
		return l
	}
	return harness
}

func ParseHarnesses(names []string) ([]string, error) {
	var want []string
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		if !slices.Contains(Harnesses, n) {
			return nil, fmt.Errorf("unknown harness %q; choose from %s", n, strings.Join(Harnesses, ", "))
		}
		want = append(want, n)
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("choose at least one harness from %s", strings.Join(Harnesses, ", "))
	}
	return slices.DeleteFunc(slices.Clone(Harnesses), func(h string) bool { return !slices.Contains(want, h) }), nil
}

type model struct {
	Name   string
	Effort string
}

type agentDef struct {
	Tier        string
	Description string
	Prompt      string
	Models      map[string]model
}

func loadAgents(source string) ([]agentDef, error) {
	dir := filepath.Join(source, "agents")
	tsv, err := os.ReadFile(filepath.Join(dir, "models.tsv"))
	if err != nil {
		return nil, err
	}
	shared, err := os.ReadFile(filepath.Join(dir, "shared.md"))
	if err != nil {
		return nil, err
	}
	var defs []agentDef
	for i, line := range strings.Split(string(tsv), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 || !slices.Contains(Harnesses, f[1]) {
			return nil, fmt.Errorf("agents/models.tsv line %d: want tier, harness (one of %v), model, effort separated by tabs", i+1, Harnesses)
		}
		at := slices.IndexFunc(defs, func(d agentDef) bool { return d.Tier == f[0] })
		if at < 0 {
			defs = append(defs, agentDef{Tier: f[0], Models: map[string]model{}})
			at = len(defs) - 1
		}
		defs[at].Models[f[1]] = model{Name: f[2], Effort: f[3]}
	}
	for i := range defs {
		if err := defs[i].load(dir, string(shared)); err != nil {
			return nil, err
		}
	}
	return defs, nil
}

func (d *agentDef) load(dir, shared string) error {
	file := "agents/" + d.Tier + ".md"
	raw, err := os.ReadFile(filepath.Join(dir, d.Tier+".md"))
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return fmt.Errorf("%s: missing frontmatter", file)
	}
	end := slices.Index(lines[1:], "---") + 1
	if end == 0 {
		return fmt.Errorf("%s: unterminated frontmatter", file)
	}
	for _, l := range lines[1:end] {
		if v, ok := strings.CutPrefix(l, "description: "); ok {
			d.Description = v
			break
		}
	}
	body := ""
	if rest := lines[end+1:]; len(rest) > 0 {
		body = strings.Join(rest, "\n") + "\n"
	}
	d.Prompt = body + "\n" + shared

	switch {
	case d.Description == "":
		return fmt.Errorf("%s: missing description", file)
	case strings.ContainsAny(d.Description, `"\`):
		return fmt.Errorf("%s: description must not contain double quotes or backslashes", file)
	case strings.Contains(d.Prompt, "'''"):
		return fmt.Errorf("%s or agents/shared.md: prompts must not contain three single quotes", file)
	}
	for _, h := range Harnesses {
		if m, ok := d.Models[h]; !ok || m.Name == "" || m.Effort == "" {
			return fmt.Errorf("agents/models.tsv lacks a model or effort for %s on %s", d.Tier, h)
		}
	}
	return nil
}

func (d agentDef) render(harness string, enabled bool) (string, error) {
	m := d.Models[harness]
	var b strings.Builder
	switch harness {
	case "claude":
		fmt.Fprintf(&b, "---\n%s\nname: %s\ndescription: \"%s\"\nmodel: %s\neffort: %s\n---\n%s", Mark, d.Tier, d.Description, m.Name, m.Effort, d.Prompt)
	case "codex":
		fmt.Fprintf(&b, "%s\nname = \"%s\"\ndescription = \"%s\"\nmodel = \"%s\"\nmodel_reasoning_effort = \"%s\"\ndeveloper_instructions = '''\n%s'''\n", Mark, d.Tier, d.Description, m.Name, m.Effort, d.Prompt)
	case "opencode":
		fmt.Fprintf(&b, "---\n%s\ndescription: \"%s\"\nmode: subagent\nmodel: %s\nreasoningEffort: %s\n", Mark, d.Description, m.Name, m.Effort)
		if !enabled {
			b.WriteString("disabled: true\n")
		}
		fmt.Fprintf(&b, "---\n%s", d.Prompt)
	default:
		return "", fmt.Errorf("unknown harness %q", harness)
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}
