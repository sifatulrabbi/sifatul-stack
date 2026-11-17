package stack_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
)

var tiers = []string{"principal", "senior", "junior", "associate"}

const sharedLine = "Use any available .planning/ dir as your scratchpad"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func newSyncer(t *testing.T, source string) stack.Syncer {
	t.Helper()
	home := t.TempDir()
	return stack.Syncer{
		Source: source,
		Layout: stack.LayoutFromEnv(home, func(string) string { return "" }),
	}
}

func run(t *testing.T, s stack.Syncer, mode stack.Mode) stack.Report {
	t.Helper()
	r, err := s.Run(mode)
	if err != nil {
		t.Fatalf("%v: %v", mode, err)
	}
	return r
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertLinksTo(t *testing.T, link, target string) {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Errorf("%s is not a symlink: %v", link, err)
		return
	}
	if got != target {
		t.Errorf("%s -> %s, want %s", link, got, target)
	}
}

func assertNoBackups(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "backup") {
			t.Errorf("%s holds a backup %s", dir, e.Name())
		}
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still exists", path)
	}
}

func hasLine(content, line string) bool {
	return slices.Contains(strings.Split(content, "\n"), line)
}

func frontmatter(t *testing.T, path string) map[string]any {
	t.Helper()
	parts := strings.SplitN(read(t, path), "\n---\n", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "---\n") {
		t.Fatalf("%s has no frontmatter", path)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(parts[0], "---\n")), &fm); err != nil {
		t.Fatalf("%s frontmatter: %v", path, err)
	}
	return fm
}

func codexAgent(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

func states(r stack.Report) map[string]string {
	out := map[string]string{}
	for _, row := range r.Rows {
		out[row.Kind+" "+row.Name+" "+row.Harness] = string(row.State)
	}
	return out
}

func TestLinkFreshHome(t *testing.T) {
	root := repoRoot(t)
	s := newSyncer(t, root)
	l := s.Layout
	write(t, filepath.Join(l.Claude, "CLAUDE.md"), "old global\n")

	if r := run(t, s, stack.Link); r.Failures() != 0 {
		t.Fatalf("failures on a fresh home: %+v", r.Rows)
	}

	skills, _ := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if len(skills) == 0 {
		t.Fatal("no skills in the repo")
	}
	for _, sk := range skills {
		dir := filepath.Dir(sk)
		assertLinksTo(t, filepath.Join(l.Claude, "skills", filepath.Base(dir)), dir)
		assertLinksTo(t, filepath.Join(l.AgentsSkills, filepath.Base(dir)), dir)
	}

	instructions := filepath.Join(root, "instructions", "AGENTS.md")
	assertLinksTo(t, filepath.Join(l.Claude, "CLAUDE.md"), instructions)
	assertLinksTo(t, filepath.Join(l.Codex, "AGENTS.md"), instructions)
	assertLinksTo(t, filepath.Join(l.OpenCode, "AGENTS.md"), instructions)
	assertNoBackups(t, l.Claude)

	for _, tier := range tiers {
		claude := filepath.Join(l.Claude, "agents", tier+".md")
		codex := filepath.Join(l.Codex, "agents", tier+".toml")
		opencode := filepath.Join(l.OpenCode, "agents", tier+".md")

		if fm := frontmatter(t, claude); fm["name"] != tier || fm["description"] == "" {
			t.Errorf("claude %s frontmatter = %v", tier, fm)
		}
		if doc := codexAgent(t, codex); doc["name"] != tier || doc["developer_instructions"] == "" {
			t.Errorf("codex %s = %v", tier, doc)
		}
		fm := frontmatter(t, opencode)
		if fm["mode"] != "subagent" || fm["disabled"] != true {
			t.Errorf("opencode %s should be a disabled subagent: %v", tier, fm)
		}
		for _, f := range []string{claude, codex, opencode} {
			if !strings.Contains(read(t, f), sharedLine) {
				t.Errorf("%s lacks the shared lines", f)
			}
		}
	}

	senior := codexAgent(t, filepath.Join(l.Codex, "agents", "senior.toml"))
	if !strings.Contains("\n"+senior["developer_instructions"].(string), "\nYou are a senior engineer") {
		t.Errorf("codex senior prompt = %q", senior["developer_instructions"])
	}
	if fm := frontmatter(t, filepath.Join(l.Claude, "agents", "senior.md")); fm["model"] != "opus" || fm["effort"] != "medium" {
		t.Errorf("claude senior = %v", fm)
	}
	if doc := codexAgent(t, filepath.Join(l.Codex, "agents", "associate.toml")); doc["model"] != "gpt-6-luna" {
		t.Errorf("codex associate model = %v", doc["model"])
	}
	if doc := codexAgent(t, filepath.Join(l.Codex, "agents", "principal.toml")); doc["model_reasoning_effort"] != "medium" {
		t.Errorf("codex principal effort = %v", doc["model_reasoning_effort"])
	}
	if fm := frontmatter(t, filepath.Join(l.OpenCode, "agents", "junior.md")); fm["model"] != "openai/gpt-6.1-terra" {
		t.Errorf("opencode junior model = %v", fm["model"])
	}

	again := run(t, s, stack.Link)
	if again.Failures() != 0 {
		t.Fatalf("second link has failures: %+v", again.Rows)
	}
	for _, row := range again.Rows {
		if row.State != stack.Linked && row.State != stack.Current {
			t.Errorf("second link changed %s %s %s: %s", row.Kind, row.Name, row.Harness, row.State)
		}
	}
}

func TestOpenCodeChoiceSurvivesRelink(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	associate := filepath.Join(s.Layout.OpenCode, "agents", "associate.md")
	write(t, associate, strings.Replace(read(t, associate), "disabled: true\n", "", 1))

	run(t, s, stack.Link)

	if hasLine(read(t, associate), "disabled: true") {
		t.Error("associate was switched back off")
	}
	if !hasLine(read(t, filepath.Join(s.Layout.OpenCode, "agents", "senior.md")), "disabled: true") {
		t.Error("senior was switched on")
	}
	for _, row := range run(t, s, stack.Status).Rows {
		if row.Kind != "agent" || row.Harness != "opencode" || (row.Name != "associate" && row.Name != "senior") {
			continue
		}
		if row.State != stack.Current || row.Enabled == nil || *row.Enabled != (row.Name == "associate") {
			t.Errorf("status of %s = %+v", row.Name, row)
		}
	}
}

func TestStaleAgentIsRerendered(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	senior := filepath.Join(s.Layout.Claude, "agents", "senior.md")
	write(t, senior, strings.Replace(read(t, senior), "model: opus\n", "model: haiku\n", 1))

	if st := states(run(t, s, stack.Status)); st["agent senior claude"] != "stale" {
		t.Errorf("status = %q, want stale", st["agent senior claude"])
	}
	if !hasLine(read(t, senior), "model: haiku") {
		t.Error("status changed the file")
	}
	run(t, s, stack.Link)
	if !hasLine(read(t, senior), "model: opus") {
		t.Error("link did not restore the model")
	}
}

func plantFile(t *testing.T, path string) { write(t, path, "mine\n") }

func plantDir(t *testing.T, path string) { write(t, filepath.Join(path, "inside"), "mine\n") }

func plantSymlink(t *testing.T, path string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "theirs")
	write(t, target, "theirs\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

var plants = map[string]func(*testing.T, string){"file": plantFile, "dir": plantDir, "symlink": plantSymlink}

func TestLinkReplacesWhateverSitsAtALinkPath(t *testing.T) {
	for what, plant := range plants {
		for _, at := range []struct{ key, path, src string }{
			{"skill full-review claude", ".claude/skills/full-review", "skills/full-review"},
			{"skill full-review agents", ".agents/skills/full-review", "skills/full-review"},
			{"instructions AGENTS.md claude", ".claude/CLAUDE.md", "instructions/AGENTS.md"},
			{"instructions AGENTS.md codex", ".codex/AGENTS.md", "instructions/AGENTS.md"},
		} {
			t.Run(what+" at "+at.path, func(t *testing.T) {
				src := fixtureSource(t, "full-review")
				home := t.TempDir()
				s := stack.Syncer{Source: src, Layout: stack.LayoutFromEnv(home, func(string) string { return "" })}
				dst := filepath.Join(home, at.path)
				plant(t, dst)
				symlinkTarget, _ := os.Readlink(dst)

				if st := states(run(t, s, stack.Status)); st[at.key] != "stale" {
					t.Errorf("status = %q, want stale", st[at.key])
				}
				if _, err := os.Lstat(dst); err != nil || isLink(dst, filepath.Join(src, at.src)) {
					t.Fatal("status changed the path")
				}
				r := run(t, s, stack.Link)

				assertLinksTo(t, dst, filepath.Join(src, at.src))
				if got := changes(r)[at.key]; got != "replaced" {
					t.Errorf("change = %q, want replaced", got)
				}
				if r.Failures() != 0 {
					t.Errorf("failures: %+v", r.Rows)
				}
				if symlinkTarget != "" && read(t, symlinkTarget) != "theirs\n" {
					t.Error("replacing a symlink changed its target")
				}
				assertNoBackups(t, filepath.Dir(dst))
			})
		}
	}
}

func isLink(path, target string) bool {
	got, err := os.Readlink(path)
	return err == nil && got == target
}

func TestUnwritableAgentDirIsAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes everywhere")
	}
	s := newSyncer(t, fixtureSource(t))
	dir := filepath.Join(s.Layout.Codex, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	r := run(t, s, stack.Link)

	if r.Failures() != len(tiers) {
		t.Errorf("failures = %d, want %d: %+v", r.Failures(), len(tiers), r.Rows)
	}
	if st := states(r); st["agent senior codex"] != "failed" || st["agent senior claude"] != "written" {
		t.Errorf("states = %v", st)
	}
}
func TestUnlinkRemovesWhatItOwnsAndRestoresNothing(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	l := s.Layout
	write(t, filepath.Join(l.Claude, "CLAUDE.md"), "old global\n")
	write(t, filepath.Join(l.Claude, "agents", "mine.md"), "keep me\n")
	run(t, s, stack.Link)

	if r := run(t, s, stack.Unlink); r.Failures() != 0 {
		t.Errorf("unlink failures: %+v", r.Rows)
	}

	assertMissing(t, filepath.Join(l.Claude, "skills", "full-review"))
	assertMissing(t, filepath.Join(l.AgentsSkills, "full-review"))
	assertMissing(t, filepath.Join(l.Claude, "agents", "senior.md"))
	assertMissing(t, filepath.Join(l.Codex, "agents", "senior.toml"))
	assertMissing(t, filepath.Join(l.OpenCode, "agents", "senior.md"))
	assertMissing(t, filepath.Join(l.Codex, "AGENTS.md"))
	if read(t, filepath.Join(l.Claude, "agents", "mine.md")) != "keep me\n" {
		t.Error("unmanaged agent was changed")
	}
	assertMissing(t, filepath.Join(l.Claude, "CLAUDE.md"))
	assertNoBackups(t, l.Claude)
}

func fixtureSource(t *testing.T, skills ...string) string {
	t.Helper()
	root := repoRoot(t)
	src := t.TempDir()
	for _, f := range []string{"agents/models.tsv", "agents/shared.md", "instructions/AGENTS.md"} {
		write(t, filepath.Join(src, f), read(t, filepath.Join(root, f)))
	}
	for _, tier := range tiers {
		f := filepath.Join("agents", tier+".md")
		write(t, filepath.Join(src, f), read(t, filepath.Join(root, f)))
	}
	for _, sk := range skills {
		write(t, filepath.Join(src, "skills", sk, "SKILL.md"), "---\nname: "+sk+"\ndescription: test\n---\n")
	}
	return src
}

func TestLinkPrunesWhatTheSourceDropped(t *testing.T) {
	src := fixtureSource(t, "keep", "gone")
	s := newSyncer(t, src)
	l := s.Layout
	s.Owned = run(t, s, stack.Link).Agents
	mine := filepath.Join(l.Claude, "agents", "mine.md")
	write(t, mine, "keep me\n")

	if err := os.RemoveAll(filepath.Join(src, "skills", "gone")); err != nil {
		t.Fatal(err)
	}
	tsv := filepath.Join(src, "agents", "models.tsv")
	var kept []string
	for _, line := range strings.Split(read(t, tsv), "\n") {
		if !strings.HasPrefix(line, "associate\t") {
			kept = append(kept, line)
		}
	}
	write(t, tsv, strings.Join(kept, "\n"))

	st := states(run(t, s, stack.Status))
	if st["skill gone claude"] != "dangling" || st["agent associate codex"] != "orphaned" {
		t.Errorf("status = %q / %q", st["skill gone claude"], st["agent associate codex"])
	}

	st = states(run(t, s, stack.Link))
	if st["skill gone claude"] != "pruned" || st["skill gone agents"] != "pruned" {
		t.Errorf("skill rows = %q / %q", st["skill gone claude"], st["skill gone agents"])
	}
	assertMissing(t, filepath.Join(l.Claude, "skills", "gone"))
	assertMissing(t, filepath.Join(l.AgentsSkills, "gone"))
	assertLinksTo(t, filepath.Join(l.Claude, "skills", "keep"), filepath.Join(src, "skills", "keep"))
	for _, f := range []string{
		filepath.Join(l.Claude, "agents", "associate.md"),
		filepath.Join(l.Codex, "agents", "associate.toml"),
		filepath.Join(l.OpenCode, "agents", "associate.md"),
	} {
		assertMissing(t, f)
	}
	if _, err := os.Stat(filepath.Join(l.Claude, "agents", "senior.md")); err != nil {
		t.Error("a current agent was pruned")
	}
	if read(t, mine) != "keep me\n" {
		t.Error("a user agent with an unrelated name was changed")
	}
}

func TestADroppedTierNobodyRecordedIsLeftAlone(t *testing.T) {
	src := fixtureSource(t)
	s := newSyncer(t, src)
	run(t, s, stack.Link)
	tsv := filepath.Join(src, "agents", "models.tsv")
	var kept []string
	for _, line := range strings.Split(read(t, tsv), "\n") {
		if !strings.HasPrefix(line, "associate\t") {
			kept = append(kept, line)
		}
	}
	write(t, tsv, strings.Join(kept, "\n"))

	run(t, s, stack.Link)

	if _, err := os.Stat(filepath.Join(s.Layout.Claude, "agents", "associate.md")); err != nil {
		t.Error("an agent the syncer was not told it owns was pruned")
	}
}

func TestReportListsTheAgentsItRendered(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	if got := run(t, s, stack.Link).Agents; !slices.Equal(got, tiers) {
		t.Errorf("link agents = %v, want %v", got, tiers)
	}
	if got := run(t, s, stack.Status).Agents; !slices.Equal(got, tiers) {
		t.Errorf("status agents = %v, want %v", got, tiers)
	}
}

func TestRenderedAgentsCarryNoMarkerLine(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	for _, dir := range []string{s.Layout.Claude, s.Layout.Codex, s.Layout.OpenCode} {
		files, _ := filepath.Glob(filepath.Join(dir, "agents", "*"))
		if len(files) != len(tiers) {
			t.Fatalf("%s holds %v", dir, files)
		}
		for _, f := range files {
			if strings.Contains(read(t, f), "managed by") {
				t.Errorf("%s carries a marker line", f)
			}
		}
	}
}

func TestLinkReplacesWhateverSitsAtAnAgentPath(t *testing.T) {
	for name, plant := range map[string]func(t *testing.T, path string){
		"file": func(t *testing.T, path string) { write(t, path, "my own senior\n") },
		"dir":  func(t *testing.T, path string) { write(t, filepath.Join(path, "inside"), "x\n") },
		"symlink": func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere.md"), path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSyncer(t, repoRoot(t))
			senior := filepath.Join(s.Layout.Claude, "agents", "senior.md")
			plant(t, senior)
			target, _ := os.Readlink(senior)

			if st := states(run(t, s, stack.Status)); st["agent senior claude"] != "stale" {
				t.Errorf("status = %q, want stale", st["agent senior claude"])
			}
			r := run(t, s, stack.Link)

			fi, err := os.Lstat(senior)
			if err != nil || !fi.Mode().IsRegular() {
				t.Fatalf("senior.md is not a regular file: %v %v", fi, err)
			}
			if fm := frontmatter(t, senior); fm["name"] != "senior" {
				t.Errorf("senior.md = %v", fm)
			}
			if target != "" {
				assertMissing(t, target)
			}
			if got := changes(r)["agent senior claude"]; got != "re-rendered" {
				t.Errorf("change = %q, want re-rendered", got)
			}
		})
	}
}

func TestUnlinkRemovesCurrentTiersWithoutARecordedList(t *testing.T) {
	s := newSyncer(t, fixtureSource(t))
	run(t, s, stack.Link)

	run(t, s, stack.Unlink)

	for _, tier := range tiers {
		assertMissing(t, filepath.Join(s.Layout.Codex, "agents", tier+".toml"))
	}
}

func TestPruneLeavesForeignFilesAlone(t *testing.T) {
	src := fixtureSource(t, "keep")
	s := newSyncer(t, src)
	l := s.Layout
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(l.Claude, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "missing"), filepath.Join(l.Claude, "skills", "theirs")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(l.Codex, "agents", "theirs.toml"), "name = \"theirs\"\n")

	run(t, s, stack.Link)
	run(t, s, stack.Unlink)

	if _, err := os.Lstat(filepath.Join(l.Claude, "skills", "theirs")); err != nil {
		t.Error("a foreign dangling link was removed")
	}
	if read(t, filepath.Join(l.Codex, "agents", "theirs.toml")) != "name = \"theirs\"\n" {
		t.Error("a foreign agent was changed")
	}
}

func TestPruneLeavesLinksIntoASiblingCheckoutAlone(t *testing.T) {
	src := fixtureSource(t, "keep")
	s := newSyncer(t, src)
	skills := filepath.Join(s.Layout.Claude, "skills")
	foreign := map[string]string{
		"mine": filepath.Join(src+"-dev", "skills", "mine"),
		"x":    filepath.Join(src, "skills-extra", "x"),
	}
	for name, target := range foreign {
		write(t, filepath.Join(target, "SKILL.md"), name+"\n")
		if err := os.MkdirAll(skills, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(skills, name)); err != nil {
			t.Fatal(err)
		}
	}

	for _, mode := range []stack.Mode{stack.Link, stack.Unlink} {
		run(t, s, mode)
		for name, target := range foreign {
			assertLinksTo(t, filepath.Join(skills, name), target)
		}
	}
}

func TestRejectsAgentTextThatBreaksQuoting(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"double quote": func(s string) string { return strings.Replace(s, "description: ", `description: a "quoted" `, 1) },
		"backslash":    func(s string) string { return strings.Replace(s, "description: ", `description: a\b `, 1) },
		"triple quote": func(s string) string { return s + "\n'''\n" },
	} {
		t.Run(name, func(t *testing.T) {
			src := fixtureSource(t)
			senior := filepath.Join(src, "agents", "senior.md")
			write(t, senior, edit(read(t, senior)))
			if _, err := newSyncer(t, src).Run(stack.Link); err == nil || !strings.Contains(err.Error(), "senior") {
				t.Errorf("err = %v, want one naming senior", err)
			}
		})
	}
}

func TestRejectsModelsTSVFieldsThatEscape(t *testing.T) {
	for name, field := range map[string]struct {
		at    int
		value string
	}{
		"tier traversal":   {0, "../config"},
		"model newline":    {2, "opus\rpermissionMode: bypassPermissions"},
		"model quote":      {2, `gpt"`},
		"model space":      {2, "gpt 5"},
		"effort quote":     {3, `high"`},
		"effort backslash": {3, `high\`},
		"empty model":      {2, ""},
	} {
		t.Run(name, func(t *testing.T) {
			src := fixtureSource(t)
			tsv := filepath.Join(src, "agents", "models.tsv")
			lines := strings.Split(read(t, tsv), "\n")
			f := strings.Split(lines[1], "\t")
			f[field.at] = field.value
			lines[1] = strings.Join(f, "\t")
			write(t, tsv, strings.Join(lines, "\n"))
			s := newSyncer(t, src)
			if _, err := s.Run(stack.Link); err == nil || !strings.Contains(err.Error(), "models.tsv") {
				t.Errorf("err = %v, want one naming models.tsv", err)
			}
			assertMissing(t, filepath.Join(s.Layout.Codex, "config.toml"))
		})
	}
}

func TestRejectsUppercaseTier(t *testing.T) {
	src := fixtureSource(t)
	tsv := filepath.Join(src, "agents", "models.tsv")
	lines := strings.Split(read(t, tsv), "\n")
	for i, l := range lines {
		if rest, ok := strings.CutPrefix(l, "senior\t"); ok {
			lines[i] = "Senior\t" + rest
		}
	}
	write(t, tsv, strings.Join(lines, "\n"))
	write(t, filepath.Join(src, "agents", "Senior.md"), read(t, filepath.Join(src, "agents", "senior.md")))

	if _, err := newSyncer(t, src).Run(stack.Link); err == nil || !strings.Contains(err.Error(), "tier must match") {
		t.Errorf("err = %v, want one saying the tier must match", err)
	}
}

func TestLayoutHonorsEnvironment(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/c", "CODEX_HOME": "/x", "XDG_CONFIG_HOME": "/cfg"}
	l := stack.LayoutFromEnv("/home/me", func(k string) string { return env[k] })
	want := stack.Layout{Claude: "/c", Codex: "/x", OpenCode: "/cfg/opencode", AgentsSkills: "/home/me/.agents/skills"}
	if l != want {
		t.Errorf("layout = %+v, want %+v", l, want)
	}
	l = stack.LayoutFromEnv("/home/me", func(string) string { return "" })
	want = stack.Layout{Claude: "/home/me/.claude", Codex: "/home/me/.codex", OpenCode: "/home/me/.config/opencode", AgentsSkills: "/home/me/.agents/skills"}
	if l != want {
		t.Errorf("default layout = %+v, want %+v", l, want)
	}
}

func changes(r stack.Report) map[string]string {
	out := map[string]string{}
	for _, row := range r.Rows {
		if row.Change != "" {
			out[row.Kind+" "+row.Name+" "+row.Harness] = string(row.Change)
		}
	}
	return out
}

func TestRowsSayWhatThisRunChanged(t *testing.T) {
	src := fixtureSource(t, "keep", "gone")
	s := newSyncer(t, src)
	write(t, filepath.Join(s.Layout.Claude, "CLAUDE.md"), "old global\n")

	first := run(t, s, stack.Link)
	for _, row := range first.Rows {
		if row.Change == "" {
			t.Errorf("fresh link left %s %s %s unmarked", row.Kind, row.Name, row.Harness)
		}
	}
	got := changes(first)
	if got["skill keep claude"] != "linked" || got["agent senior codex"] != "written" || got["instructions AGENTS.md codex"] != "linked" {
		t.Errorf("fresh link changes = %v", got)
	}
	if got["instructions AGENTS.md claude"] != "replaced" {
		t.Errorf("claude instructions change = %q, want replaced", got["instructions AGENTS.md claude"])
	}

	if got := changes(run(t, s, stack.Link)); len(got) != 0 {
		t.Errorf("relink changed %v", got)
	}

	senior := filepath.Join(s.Layout.Claude, "agents", "senior.md")
	write(t, senior, strings.Replace(read(t, senior), "model: opus\n", "model: haiku\n", 1))
	if got := changes(run(t, s, stack.Status)); len(got) != 0 {
		t.Errorf("status reported changes %v", got)
	}
	if err := os.RemoveAll(filepath.Join(src, "skills", "gone")); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"agent senior claude": "re-rendered", "skill gone claude": "pruned", "skill gone agents": "pruned"}
	if got := changes(run(t, s, stack.Link)); !maps.Equal(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}

	got = changes(run(t, s, stack.Unlink))
	if got["skill keep claude"] != "removed" || got["agent senior opencode"] != "removed" || got["instructions AGENTS.md claude"] != "removed" || got["instructions AGENTS.md codex"] != "removed" {
		t.Errorf("unlink changes = %v", got)
	}
}

func harnessesIn(r stack.Report) []string {
	var out []string
	for _, row := range r.Rows {
		if !slices.Contains(out, row.Harness) {
			out = append(out, row.Harness)
		}
	}
	slices.Sort(out)
	return out
}

func TestLinkTouchesOnlySelectedHarnesses(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	s.Selected = []string{"claude"}
	l := s.Layout

	r := run(t, s, stack.Link)

	if got := harnessesIn(r); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("rows cover %v, want only claude", got)
	}
	assertLinksTo(t, filepath.Join(l.Claude, "CLAUDE.md"), filepath.Join(s.Source, "instructions", "AGENTS.md"))
	if _, err := os.Stat(filepath.Join(l.Claude, "agents", "senior.md")); err != nil {
		t.Error("claude agents were not rendered")
	}
	for _, p := range []string{l.AgentsSkills, l.Codex, l.OpenCode} {
		assertMissing(t, p)
	}
}

func TestSharedSkillsDirFollowsCodexOrOpenCode(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	s.Selected = []string{"opencode"}
	run(t, s, stack.Link)

	assertLinksTo(t, filepath.Join(s.Layout.AgentsSkills, "full-review"), filepath.Join(s.Source, "skills", "full-review"))
	assertMissing(t, filepath.Join(s.Layout.Claude, "skills"))
}

func TestDeselectedHarnessIsRemovedOnLink(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	l := s.Layout
	codexMD := filepath.Join(l.Codex, "AGENTS.md")
	write(t, codexMD, "old codex global\n")
	run(t, s, stack.Link)

	s.Selected = []string{"claude", "opencode"}
	r := run(t, s, stack.Link)

	assertMissing(t, codexMD)
	assertMissing(t, filepath.Join(l.Codex, "agents", "senior.toml"))
	assertLinksTo(t, filepath.Join(l.AgentsSkills, "full-review"), filepath.Join(s.Source, "skills", "full-review"))
	if got := changes(r)["agent senior codex"]; got != "removed" {
		t.Errorf("codex senior change = %q, want removed", got)
	}

	s.Selected = []string{"claude"}
	run(t, s, stack.Link)
	assertMissing(t, filepath.Join(l.AgentsSkills, "full-review"))
	assertMissing(t, filepath.Join(l.OpenCode, "agents", "senior.md"))
	assertLinksTo(t, filepath.Join(l.Claude, "skills", "full-review"), filepath.Join(s.Source, "skills", "full-review"))
}

func TestStatusReportsOnlySelectedHarnesses(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	s.Selected = []string{"codex"}

	r := run(t, s, stack.Status)

	if got := harnessesIn(r); !slices.Equal(got, []string{"agents", "codex"}) {
		t.Errorf("status covers %v, want agents and codex", got)
	}
	if _, err := os.Stat(filepath.Join(s.Layout.Claude, "agents", "senior.md")); err != nil {
		t.Error("status removed an unselected harness's files")
	}
}

func TestUnlinkRemovesEveryHarnessWhateverTheSelection(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	s.Selected = []string{"claude"}

	run(t, s, stack.Unlink)

	assertMissing(t, filepath.Join(s.Layout.Codex, "agents", "senior.toml"))
	assertMissing(t, filepath.Join(s.Layout.AgentsSkills, "full-review"))
	assertMissing(t, filepath.Join(s.Layout.Claude, "CLAUDE.md"))
}

func TestParseHarnesses(t *testing.T) {
	got, err := stack.ParseHarnesses([]string{"opencode", " Claude ", "opencode"})
	if err != nil || !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("got %v %v, want [claude opencode] in canonical order without duplicates", got, err)
	}
	if _, err := stack.ParseHarnesses([]string{"cursor"}); err == nil || !strings.Contains(err.Error(), "claude, codex, opencode") {
		t.Errorf("unknown harness error = %v", err)
	}
	if _, err := stack.ParseHarnesses(nil); err == nil {
		t.Error("an empty selection should be refused")
	}
}

func TestLinkReplacesAnUnreadableAgent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	s := newSyncer(t, repoRoot(t))
	foreign := filepath.Join(s.Layout.Claude, "agents", "senior.md")
	write(t, foreign, "my own senior\n")
	if err := os.Chmod(foreign, 0o200); err != nil {
		t.Fatal(err)
	}

	r := run(t, s, stack.Link)

	if err := os.Chmod(foreign, 0o644); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if fm := frontmatter(t, foreign); fm["name"] != "senior" {
		t.Errorf("senior.md = %v", fm)
	}
	if r.Failures() != 0 {
		t.Errorf("failures = %d, want 0: %+v", r.Failures(), r.Rows)
	}
}

func TestUnlinkWorksAfterTheSourceIsGone(t *testing.T) {
	src := fixtureSource(t, "keep")
	s := newSyncer(t, src)
	l := s.Layout
	s.Owned = run(t, s, stack.Link).Agents
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}

	if r := run(t, s, stack.Unlink); r.Failures() != 0 {
		t.Errorf("unlink failures: %+v", r.Rows)
	}

	for _, p := range []string{
		filepath.Join(l.Claude, "skills", "keep"),
		filepath.Join(l.AgentsSkills, "keep"),
		filepath.Join(l.Claude, "CLAUDE.md"),
		filepath.Join(l.Codex, "agents", "senior.toml"),
		filepath.Join(l.OpenCode, "agents", "junior.md"),
	} {
		assertMissing(t, p)
	}
}

func TestOpenCodeChoiceSurvivesDetachThenLink(t *testing.T) {
	s := newSyncer(t, fixtureSource(t, "keep"))
	run(t, s, stack.Link)
	senior := filepath.Join(s.Layout.OpenCode, "agents", "senior.md")
	write(t, senior, strings.Replace(read(t, senior), "disabled: true\n", "", 1))

	run(t, s, stack.Detach)
	assertMissing(t, filepath.Join(s.Layout.Claude, "skills", "keep"))
	assertMissing(t, filepath.Join(s.Layout.Codex, "AGENTS.md"))
	s.Source = fixtureSource(t, "keep")
	run(t, s, stack.Link)

	if hasLine(read(t, senior), "disabled: true") {
		t.Error("senior was switched back off after a source switch")
	}
	assertLinksTo(t, filepath.Join(s.Layout.Claude, "skills", "keep"), filepath.Join(s.Source, "skills", "keep"))
}

func TestRunRefusesAnUnknownHarness(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	s.Selected = []string{"claude", "gemini"}

	for _, mode := range []stack.Mode{stack.Link, stack.Status} {
		if _, err := s.Run(mode); err == nil || !strings.Contains(err.Error(), "gemini") {
			t.Errorf("%s err = %v, want one naming gemini", mode, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Layout.OpenCode, "agents", "senior.md")); err != nil {
		t.Error("an unknown harness removed OpenCode's agents")
	}
}

func TestUnlinkLeavesForeignGlobalInstructionsAlone(t *testing.T) {
	s := newSyncer(t, fixtureSource(t))
	run(t, s, stack.Link)
	for _, path := range []string{filepath.Join(s.Layout.Claude, "CLAUDE.md"), filepath.Join(s.Layout.Codex, "AGENTS.md")} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		write(t, path, "hand written\n")
	}

	run(t, s, stack.Unlink)

	for _, path := range []string{filepath.Join(s.Layout.Claude, "CLAUDE.md"), filepath.Join(s.Layout.Codex, "AGENTS.md")} {
		if got := read(t, path); got != "hand written\n" {
			t.Errorf("%s = %q", path, got)
		}
	}
}

func TestUnlinkIgnoresAnUnknownSavedHarness(t *testing.T) {
	for _, mode := range []stack.Mode{stack.Unlink, stack.Detach} {
		t.Run(string(mode), func(t *testing.T) {
			s := newSyncer(t, fixtureSource(t, "keep"))
			run(t, s, stack.Link)
			s.Selected = []string{"claude", "gemini"}

			if _, err := s.Run(mode); err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			assertMissing(t, filepath.Join(s.Layout.Claude, "skills", "keep"))
		})
	}
}

func TestUpdateKeepsUserCopyOfRenderedAgent(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	s.Owned = run(t, s, stack.Link).Agents
	copies := map[string]string{}
	for from, to := range map[string]string{
		filepath.Join(s.Layout.Claude, "agents", "senior.md"):   filepath.Join(s.Layout.Claude, "agents", "my-reviewer.md"),
		filepath.Join(s.Layout.Codex, "agents", "senior.toml"):  filepath.Join(s.Layout.Codex, "agents", "my-reviewer.toml"),
		filepath.Join(s.Layout.OpenCode, "agents", "senior.md"): filepath.Join(s.Layout.OpenCode, "agents", "my-reviewer.md"),
	} {
		copies[to] = read(t, from) + "my own edits\n"
		write(t, to, copies[to])
	}

	for _, mode := range []stack.Mode{stack.Link, stack.Unlink} {
		run(t, s, mode)
		for path, want := range copies {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("%s deleted the user's copy %s", mode, path)
			}
			if got := read(t, path); got != want {
				t.Errorf("%s changed %s", mode, path)
			}
		}
	}
	for _, p := range []string{
		filepath.Join(s.Layout.Claude, "agents", "senior.md"),
		filepath.Join(s.Layout.Codex, "agents", "senior.toml"),
		filepath.Join(s.Layout.OpenCode, "agents", "senior.md"),
	} {
		assertMissing(t, p)
	}
}

func TestLinkRewritesAgentsRenderedWithTheOldMarkerLine(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	run(t, s, stack.Link)
	opencode := filepath.Join(s.Layout.OpenCode, "agents", "senior.md")
	codex := filepath.Join(s.Layout.Codex, "agents", "senior.toml")
	write(t, opencode, strings.Replace(strings.Replace(read(t, opencode), "---\n", "---\n# managed by sifatul-stack\n", 1), "disabled: true\n", "", 1))
	write(t, codex, "# managed by sifatul-stack\n"+read(t, codex))

	if st := states(run(t, s, stack.Status)); st["agent senior opencode"] != "stale" || st["agent senior codex"] != "stale" {
		t.Errorf("status = %v", st)
	}
	run(t, s, stack.Link)

	for _, f := range []string{opencode, codex} {
		if strings.Contains(read(t, f), "managed by") {
			t.Errorf("%s kept the marker line", f)
		}
	}
	if hasLine(read(t, opencode), "disabled: true") {
		t.Error("the user's OpenCode choice was lost in the rewrite")
	}
}
