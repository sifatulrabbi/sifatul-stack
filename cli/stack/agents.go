package stack

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

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

var (
	tierName   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	modelValue = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
)

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
		if len(f) != 4 || !slices.Contains(keys(table), f[1]) {
			return nil, fmt.Errorf("agents/models.tsv line %d: want tier, harness (one of %v), model, effort separated by tabs", i+1, keys(table))
		}
		if !tierName.MatchString(f[0]) || !modelValue.MatchString(f[2]) || !modelValue.MatchString(f[3]) {
			return nil, fmt.Errorf("agents/models.tsv line %d: tier must match %s, model and effort %s", i+1, tierName, modelValue)
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
	for _, h := range keys(table) {
		if m, ok := d.Models[h]; !ok || m.Name == "" || m.Effort == "" {
			return fmt.Errorf("agents/models.tsv lacks a model or effort for %s on %s", d.Tier, h)
		}
	}
	return nil
}

func finish(b *strings.Builder) string { return strings.TrimRight(b.String(), "\n") + "\n" }

func renderClaude(d agentDef, m model, _ bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\n%s\nname: %s\ndescription: \"%s\"\nmodel: %s\neffort: %s\n---\n%s", Mark, d.Tier, d.Description, m.Name, m.Effort, d.Prompt)
	return finish(&b)
}

func claudeName(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	if lines[0] != "---" {
		return "", false
	}
	for _, l := range lines[1:] {
		if l == "---" {
			break
		}
		if name, ok := strings.CutPrefix(l, "name: "); ok {
			return name, true
		}
	}
	return "", false
}

func renderCodex(d agentDef, m model, _ bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nname = \"%s\"\ndescription = \"%s\"\nmodel = \"%s\"\nmodel_reasoning_effort = \"%s\"\ndeveloper_instructions = '''\n%s'''\n", Mark, d.Tier, d.Description, m.Name, m.Effort, d.Prompt)
	return finish(&b)
}

func codexName(raw string) (string, bool) {
	for _, l := range strings.Split(raw, "\n") {
		if strings.HasPrefix(l, "developer_instructions") {
			break
		}
		if quoted, ok := strings.CutPrefix(l, "name = "); ok {
			name, err := strconv.Unquote(quoted)
			return name, err == nil
		}
	}
	return "", false
}

func renderOpenCode(d agentDef, m model, enabled bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\n%s\ndescription: \"%s\"\nmode: subagent\nmodel: %s\nreasoningEffort: %s\n", Mark, d.Description, m.Name, m.Effort)
	if !enabled {
		b.WriteString("disabled: true\n")
	}
	fmt.Fprintf(&b, "---\n%s", d.Prompt)
	return finish(&b)
}

func openCodeEnabled(raw string) bool {
	return !slices.Contains(strings.Split(raw, "\n"), "disabled: true")
}
