package stack

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Mode string

const (
	Link   Mode = "link"
	Unlink Mode = "unlink"
	Status Mode = "status"
	Detach Mode = "detach"
)

func (m Mode) releases() bool { return m == Unlink || m == Detach }

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

type State string

const (
	Linked   State = "linked"
	Current  State = "current"
	Missing  State = "missing"
	Stale    State = "stale"
	Written  State = "written"
	Dangling State = "dangling"
	Orphaned State = "orphaned"
	Removed  State = "removed"
	Pruned   State = "pruned"
	Failed   State = "failed"
)

type Change string

const (
	DidLink     Change = "linked"
	DidWrite    Change = "written"
	DidRerender Change = "re-rendered"
	DidReplace  Change = "replaced"
	DidPrune    Change = "pruned"
	DidRemove   Change = "removed"
)

type Row struct {
	Kind    string
	Name    string
	Harness string
	State   State
	Change  Change
	Enabled *bool
	Detail  string
}

type Report struct {
	Rows   []Row
	Agents []string
}

func (r Report) Failures() int {
	n := 0
	for _, row := range r.Rows {
		if row.State == Failed {
			n++
		}
	}
	return n
}

type Syncer struct {
	Source    string
	Layout    Layout
	Selected  []string
	Dropped   []string
	Owned     []string
	ExtrasDir string
	Extras    []Extra
}

type run struct {
	Syncer
	mode   Mode
	report *Report
}

func (s Syncer) selected(harness string) bool {
	return slices.Contains(Chosen(s.Selected), harness)
}

func (s Syncer) dropped(harness string) bool {
	return slices.Contains(s.Dropped, harness)
}

func (r *run) on(harnesses ...string) *run {
	switch {
	case slices.ContainsFunc(harnesses, r.selected):
		return r
	case r.mode == Link && slices.ContainsFunc(harnesses, r.dropped):
		return &run{Syncer: r.Syncer, mode: Unlink, report: r.report}
	default:
		return nil
	}
}

func (s Syncer) Run(mode Mode) (Report, error) {
	if !mode.releases() {
		for _, h := range s.Selected {
			if _, err := Find(h); err != nil {
				return Report{}, err
			}
		}
	}
	var defs []agentDef
	var skills []skillLink
	if !mode.releases() {
		var err error
		if defs, err = loadAgents(s.Source); err != nil {
			return Report{}, err
		}
		if skills, err = s.skillLinks(); err != nil {
			return Report{}, err
		}
	} else if mode == Unlink {
		defs, _ = loadAgents(s.Source)
	}
	r := &run{Syncer: s, mode: mode, report: &Report{}}
	if !mode.releases() {
		for _, d := range defs {
			r.report.Agents = append(r.report.Agents, d.Tier)
		}
	}
	for _, g := range skillsGroups() {
		x := r.on(g.users...)
		if x == nil {
			continue
		}
		dir := g.dir(s.Layout)
		if !x.mode.releases() {
			for _, l := range skills {
				switch {
				case l.err == nil:
					x.linkPath("skill", l.name, g.key, l.src, filepath.Join(dir, l.name))
				case x.mode == Status && errors.Is(l.err, errNotCloned):
					x.row("skill", l.name, g.key, Missing)
				default:
					x.fail("skill", l.name, g.key, l.err)
				}
			}
		}
		x.pruneSkills(g.key, dir, skills)
	}

	for _, h := range table {
		if x := r.on(h.Key); x != nil {
			x.linkPath("instructions", "AGENTS.md", h.Key, filepath.Join(s.Source, "instructions", "AGENTS.md"), h.instructionsPath(s.Layout))
		}
	}

	for _, d := range defs {
		for _, h := range table {
			if x := r.on(h.Key); x != nil && !x.mode.releases() {
				x.syncAgent(d, h)
			}
		}
	}
	for _, h := range table {
		if x := r.on(h.Key); x != nil && x.mode != Detach {
			x.pruneAgents(h, defs)
		}
	}
	return *r.report, nil
}

type skillsTarget struct {
	skillsGroup
	users []string
}

func skillsGroups() []skillsTarget {
	var out []skillsTarget
	for _, h := range table {
		i := slices.IndexFunc(out, func(t skillsTarget) bool { return t.key == h.skills.key })
		if i < 0 {
			out = append(out, skillsTarget{skillsGroup: h.skills})
			i = len(out) - 1
		}
		out[i].users = append(out[i].users, h.Key)
	}
	return out
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

func (r *run) row(kind, name, harness string, state State) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: state})
}

func (r *run) did(kind, name, harness string, state State, change Change, detail string) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: state, Change: change, Detail: detail})
}

func (r *run) fail(kind, name, harness string, err error) {
	r.report.Rows = append(r.report.Rows, Row{Kind: kind, Name: name, Harness: harness, State: Failed, Detail: err.Error()})
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
		if !r.mode.releases() {
			r.row(kind, name, harness, Linked)
			return
		}
		if err := os.Remove(dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		r.did(kind, name, harness, Removed, DidRemove, "")
		return
	}
	present := exists(dst)
	switch {
	case r.mode.releases():
	case r.mode == Status && present:
		r.did(kind, name, harness, Stale, "", dst)
	case r.mode == Status:
		r.row(kind, name, harness, Missing)
	default:
		if err := replaceWithLink(src, dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		if present {
			r.did(kind, name, harness, Linked, DidReplace, "")
			return
		}
		r.did(kind, name, harness, Linked, DidLink, "")
	}
}

func replaceWithLink(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Symlink(src, dst)
}

func (r *run) ownsSkillLink(target string) bool {
	roots := []string{filepath.Join(r.Source, "skills")}
	if r.ExtrasDir != "" {
		roots = append(roots, r.ExtrasDir)
	}
	return slices.ContainsFunc(roots, func(root string) bool {
		return strings.HasPrefix(target, root+string(filepath.Separator))
	})
}

func (r *run) pruneSkills(harness, dir string, current []skillLink) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		target, err := os.Readlink(path)
		if err != nil || !r.ownsSkillLink(target) {
			continue
		}
		if !r.mode.releases() && slices.ContainsFunc(current, func(l skillLink) bool {
			return l.err == nil && l.name == e.Name() && isLinkTo(path, l.src)
		}) {
			continue
		}
		r.retire("skill", e.Name(), harness, path, Dangling)
	}
}

func (r *run) retire(kind, name, harness, path string, statusState State) {
	switch r.mode {
	case Status:
		r.row(kind, name, harness, statusState)
	default:
		if err := os.RemoveAll(path); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		if r.mode.releases() {
			r.did(kind, name, harness, Removed, DidRemove, "")
			return
		}
		r.did(kind, name, harness, Pruned, DidPrune, "")
	}
}

func readAgent(path string) (raw string, regular bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	b, err := os.ReadFile(path)
	return string(b), err == nil
}

func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (r *run) syncAgent(d agentDef, h Harness) {
	dst := h.agentPath(r.Layout, d.Tier)
	text, readable := readAgent(dst)
	present := exists(dst)
	enabled := h.enabledIn == nil || (readable && h.enabledIn(text))
	wanted := h.render(d, d.Models[h.Key], enabled)
	row := Row{Kind: "agent", Name: d.Tier, Harness: h.Key, State: Stale}
	if h.enabledIn != nil {
		row.Enabled = &enabled
	}
	switch {
	case !present:
		row.State = Missing
	case readable && text == wanted:
		row.State = Current
	}
	if r.mode == Link && row.State != Current {
		if err := writeAgent(dst, wanted); err != nil {
			r.fail("agent", d.Tier, h.Key, err)
			return
		}
		row.Change = map[State]Change{Missing: DidWrite, Stale: DidRerender}[row.State]
		row.State = Written
	}
	r.report.Rows = append(r.report.Rows, row)
}

func writeAgent(dst, wanted string) error {
	if fi, err := os.Lstat(dst); err == nil && !fi.Mode().IsRegular() {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return writeFile(dst, []byte(wanted))
}

func (r *run) pruneAgents(h Harness, defs []agentDef) {
	current := make([]string, len(defs))
	for i, d := range defs {
		current[i] = d.Tier
	}
	var candidates []string
	for _, tier := range append(slices.Clone(r.Owned), current...) {
		if slices.Contains(candidates, tier) || !r.mode.releases() && slices.Contains(current, tier) {
			continue
		}
		candidates = append(candidates, tier)
	}
	for _, tier := range candidates {
		path := h.agentPath(r.Layout, tier)
		if !exists(path) {
			continue
		}
		r.retire("agent", tier, h.Key, path, Orphaned)
		if r.mode != Status && exists(path) && !slices.Contains(r.report.Agents, tier) {
			r.report.Agents = append(r.report.Agents, tier)
		}
	}
}
