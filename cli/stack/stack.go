package stack

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const Mark = "# managed by sifatul-stack"

type Mode string

const (
	Link   Mode = "link"
	Unlink Mode = "unlink"
	Status Mode = "status"
)

type Layout struct {
	Claude       string
	Codex        string
	OpenCode     string
	AgentsSkills string
}

func LayoutFromEnv(home string, getenv func(string) string) Layout {
	or := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	return Layout{
		Claude:       or("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")),
		Codex:        or("CODEX_HOME", filepath.Join(home, ".codex")),
		OpenCode:     filepath.Join(or("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode"),
		AgentsSkills: filepath.Join(home, ".agents", "skills"),
	}
}

type Row struct {
	Kind     string
	Name     string
	Harness  string
	State    string
	Change   string
	Conflict bool
}

type Report struct {
	Rows []Row
}

func (r Report) Conflicts() int {
	n := 0
	for _, row := range r.Rows {
		if row.Conflict {
			n++
		}
	}
	return n
}

type Syncer struct {
	Source string
	Layout Layout
	Now    func() time.Time
}

type run struct {
	Syncer
	mode   Mode
	report Report
}

func (s Syncer) Run(mode Mode) (Report, error) {
	defs, err := loadAgents(s.Source)
	if err != nil {
		return Report{}, err
	}
	skills, err := s.skills()
	if err != nil {
		return Report{}, err
	}
	r := &run{Syncer: s, mode: mode}
	for _, name := range skills {
		src := filepath.Join(s.Source, "skills", name)
		r.linkPath("skill", name, "claude", src, filepath.Join(s.Layout.Claude, "skills", name))
		r.linkPath("skill", name, "agents", src, filepath.Join(s.Layout.AgentsSkills, name))
	}
	r.pruneSkills("claude", filepath.Join(s.Layout.Claude, "skills"), skills)
	r.pruneSkills("agents", s.Layout.AgentsSkills, skills)

	r.linkInstructions("claude", filepath.Join(s.Layout.Claude, "CLAUDE.md"))
	r.linkInstructions("codex", filepath.Join(s.Layout.Codex, "AGENTS.md"))
	r.linkInstructions("opencode", filepath.Join(s.Layout.OpenCode, "AGENTS.md"))

	for _, d := range defs {
		for _, h := range harnesses {
			if err := r.syncAgent(d, h); err != nil {
				return r.report, err
			}
		}
	}
	for _, h := range harnesses {
		r.pruneAgents(h, defs)
	}
	return r.report, nil
}

func (s Syncer) skills() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Source, "skills"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (r *run) row(kind, name, harness, state string) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: state})
}

func (r *run) did(kind, name, harness, state, change string) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: state, Change: change})
}

func (r *run) conflict(kind, name, harness, why string) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: "conflict: " + why, Conflict: true})
}

func (r *run) fail(kind, name, harness string, err error) {
	r.conflict(kind, name, harness, err.Error())
}

func isLinkTo(path, target string) bool {
	got, err := os.Readlink(path)
	return err == nil && got == target
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (r *run) linkPath(kind, name, harness, src, dst string) {
	if isLinkTo(dst, src) {
		if r.mode != Unlink {
			r.row(kind, name, harness, "linked")
			return
		}
		if err := os.Remove(dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		r.did(kind, name, harness, "removed", "removed")
		return
	}
	switch {
	case r.mode == Unlink:
	case exists(dst):
		r.conflict(kind, name, harness, dst+" exists")
	case r.mode == Status:
		r.row(kind, name, harness, "missing")
	default:
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		if err := os.Symlink(src, dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		r.did(kind, name, harness, "linked", "linked")
	}
}

func (r *run) pruneSkills(harness, dir string, current []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	owned := filepath.Join(r.Source, "skills") + string(filepath.Separator)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		target, err := os.Readlink(path)
		if err != nil || !strings.HasPrefix(target, owned) {
			continue
		}
		if r.mode != Unlink && slices.Contains(current, e.Name()) && isLinkTo(path, filepath.Join(r.Source, "skills", e.Name())) {
			continue
		}
		r.retire("skill", e.Name(), harness, path, "dangling")
	}
}

func (r *run) retire(kind, name, harness, path, statusState string) {
	switch r.mode {
	case Status:
		r.row(kind, name, harness, statusState)
	default:
		if err := os.Remove(path); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		state := "pruned"
		if r.mode == Unlink {
			state = "removed"
		}
		r.did(kind, name, harness, state, state)
	}
}

func (r *run) linkInstructions(harness, dst string) {
	src := filepath.Join(r.Source, "instructions", "AGENTS.md")
	const kind, name = "instructions", "AGENTS.md"
	if r.mode == Unlink {
		if !isLinkTo(dst, src) {
			return
		}
		if err := os.Remove(dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		backups, _ := filepath.Glob(dst + ".backup-*")
		if len(backups) == 0 {
			r.did(kind, name, harness, "removed", "removed")
			return
		}
		sort.Strings(backups)
		newest := backups[len(backups)-1]
		if err := os.Rename(newest, dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		r.did(kind, name, harness, "restored "+filepath.Base(newest), "restored")
		return
	}
	if fi, err := os.Lstat(dst); err == nil && fi.Mode().IsRegular() {
		if r.mode == Status {
			r.row(kind, name, harness, "missing (link backs up "+dst+")")
			return
		}
		backup := dst + ".backup-" + r.Now().Format("20060102150405")
		if err := os.Rename(dst, backup); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		r.did(kind, name, harness, "backed up to "+filepath.Base(backup), "backed up")
	}
	r.linkPath(kind, name, harness, src, dst)
}

func (r *run) agentPath(tier, harness string) string {
	switch harness {
	case "claude":
		return filepath.Join(r.Layout.Claude, "agents", tier+".md")
	case "codex":
		return filepath.Join(r.Layout.Codex, "agents", tier+".toml")
	default:
		return filepath.Join(r.Layout.OpenCode, "agents", tier+".md")
	}
}

func managed(content string) bool {
	return slices.Contains(strings.Split(content, "\n"), Mark)
}

func (r *run) syncAgent(d agentDef, harness string) error {
	dst := r.agentPath(d.Tier, harness)
	raw, err := os.ReadFile(dst)
	present := err == nil
	if present && !managed(string(raw)) {
		if r.mode != Unlink {
			r.conflict("agent", d.Tier, harness, dst+" is not managed by sifatul-stack")
		}
		return nil
	}
	if r.mode == Unlink {
		if present {
			if err := os.Remove(dst); err != nil {
				r.fail("agent", d.Tier, harness, err)
				return nil
			}
			r.did("agent", d.Tier, harness, "removed", "removed")
		}
		return nil
	}
	enabled := harness != "opencode" || (present && !slices.Contains(strings.Split(string(raw), "\n"), "disabled: true"))
	wanted, err := d.render(harness, enabled)
	if err != nil {
		return err
	}
	state := "stale"
	switch {
	case !present:
		state = "missing"
	case string(raw) == wanted:
		state = "current"
	}
	change := ""
	if r.mode == Link && state != "current" {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			r.fail("agent", d.Tier, harness, err)
			return nil
		}
		if err := os.WriteFile(dst, []byte(wanted), 0o644); err != nil {
			r.fail("agent", d.Tier, harness, err)
			return nil
		}
		change = map[string]string{"missing": "written", "stale": "re-rendered"}[state]
		state = "written"
	}
	if harness == "opencode" {
		state = map[bool]string{true: "on", false: "off"}[enabled] + ", " + state
	}
	r.did("agent", d.Tier, harness, state, change)
	return nil
}

func (r *run) pruneAgents(harness string, defs []agentDef) {
	dir := filepath.Dir(r.agentPath("x", harness))
	ext := filepath.Ext(r.agentPath("x", harness))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		tier, ok := strings.CutSuffix(e.Name(), ext)
		if !ok || e.IsDir() || slices.ContainsFunc(defs, func(d agentDef) bool { return d.Tier == tier }) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil || !managed(string(raw)) {
			continue
		}
		r.retire("agent", tier, harness, path, "orphaned")
	}
}
