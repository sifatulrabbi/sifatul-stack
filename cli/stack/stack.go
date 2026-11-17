package stack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const Mark = "# managed by sifatul-stack"

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
	Source   string
	Layout   Layout
	Now      func() time.Time
	Selected []string
}

type run struct {
	Syncer
	mode   Mode
	report *Report
}

func (s Syncer) selected(harness string) bool {
	return slices.Contains(Chosen(s.Selected), harness)
}

func (r *run) on(harnesses ...string) *run {
	switch {
	case r.mode.releases() || slices.ContainsFunc(harnesses, r.selected):
		return r
	case r.mode == Link:
		return &run{Syncer: r.Syncer, mode: Unlink, report: r.report}
	default:
		return nil
	}
}

func (s Syncer) Run(mode Mode) (Report, error) {
	if mode != Unlink {
		for _, h := range s.Selected {
			if _, err := Find(h); err != nil {
				return Report{}, err
			}
		}
	}
	var defs []agentDef
	var skills []string
	if !mode.releases() {
		var err error
		if defs, err = loadAgents(s.Source); err != nil {
			return Report{}, err
		}
		if skills, err = s.skills(); err != nil {
			return Report{}, err
		}
	}
	r := &run{Syncer: s, mode: mode, report: &Report{}}
	for _, g := range skillsGroups() {
		x := r.on(g.users...)
		if x == nil {
			continue
		}
		dir := g.dir(s.Layout)
		if !x.mode.releases() {
			for _, name := range skills {
				x.linkPath("skill", name, g.key, filepath.Join(s.Source, "skills", name), filepath.Join(dir, name))
			}
		}
		x.pruneSkills(g.key, dir, skills)
	}

	for _, h := range table {
		if x := r.on(h.Key); x != nil {
			x.linkInstructions(h.Key, h.instructionsPath(s.Layout))
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
		if !r.mode.releases() {
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
	case r.mode.releases():
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
		if !r.mode.releases() && slices.Contains(current, e.Name()) && isLinkTo(path, filepath.Join(r.Source, "skills", e.Name())) {
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
		if r.mode.releases() {
			state = "removed"
		}
		r.did(kind, name, harness, state, state)
	}
}

func (r *run) linkInstructions(harness, dst string) {
	src := filepath.Join(r.Source, "instructions", "AGENTS.md")
	const kind, name = "instructions", "AGENTS.md"
	if r.mode.releases() {
		if !isLinkTo(dst, src) {
			return
		}
		if err := os.Remove(dst); err != nil {
			r.fail(kind, name, harness, err)
			return
		}
		newest := newestBackup(dst)
		if newest == "" {
			r.did(kind, name, harness, "removed", "removed")
			return
		}
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

const backupStamp = "20060102150405"

func newestBackup(dst string) string {
	entries, _ := os.ReadDir(filepath.Dir(dst))
	newest, at := "", time.Time{}
	for _, e := range entries {
		stamp, ok := strings.CutPrefix(e.Name(), filepath.Base(dst)+".backup-")
		t, err := time.Parse(backupStamp, stamp)
		if ok && err == nil && (newest == "" || t.After(at)) {
			newest, at = filepath.Join(filepath.Dir(dst), e.Name()), t
		}
	}
	return newest
}

func managed(content string) bool {
	return slices.Contains(strings.Split(content, "\n"), Mark)
}

func readAgent(path string) (raw string, present bool, err error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", true, err
	case !fi.Mode().IsRegular():
		return "", true, fmt.Errorf("%s is not a regular file", path)
	}
	b, err := os.ReadFile(path)
	return string(b), true, err
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
	text, present, err := readAgent(dst)
	if err != nil {
		r.conflict("agent", d.Tier, h.Key, err.Error())
		return
	}
	if present && !managed(text) {
		r.conflict("agent", d.Tier, h.Key, dst+" is not managed by sifatul-stack")
		return
	}
	enabled := !h.toggles || (present && !slices.Contains(strings.Split(text, "\n"), "disabled: true"))
	wanted := h.render(d, d.Models[h.Key], enabled)
	state := "stale"
	switch {
	case !present:
		state = "missing"
	case text == wanted:
		state = "current"
	}
	change := ""
	if r.mode == Link && state != "current" {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			r.fail("agent", d.Tier, h.Key, err)
			return
		}
		if err := writeFile(dst, []byte(wanted)); err != nil {
			r.fail("agent", d.Tier, h.Key, err)
			return
		}
		change = map[string]string{"missing": "written", "stale": "re-rendered"}[state]
		state = "written"
	}
	if h.toggles {
		state = map[bool]string{true: "on", false: "off"}[enabled] + ", " + state
	}
	r.did("agent", d.Tier, h.Key, state, change)
}

func (r *run) pruneAgents(h Harness, defs []agentDef) {
	dir := h.agentDir(r.Layout)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		tier, ok := strings.CutSuffix(e.Name(), h.agentExt)
		if !ok || !e.Type().IsRegular() || !r.mode.releases() && slices.ContainsFunc(defs, func(d agentDef) bool { return d.Tier == tier }) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil || !managed(string(raw)) {
			continue
		}
		r.retire("agent", tier, h.Key, path, "orphaned")
	}
}
