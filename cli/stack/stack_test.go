package stack_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func fixedNow() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

func newSyncer(t *testing.T, source string) stack.Syncer {
	t.Helper()
	home := t.TempDir()
	return stack.Syncer{
		Source: source,
		Layout: stack.LayoutFromEnv(home, func(string) string { return "" }),
		Now:    fixedNow,
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
		out[row.Kind+" "+row.Name+" "+row.Harness] = row.State
	}
	return out
}

func TestLinkFreshHome(t *testing.T) {
	root := repoRoot(t)
	s := newSyncer(t, root)
	l := s.Layout
	write(t, filepath.Join(l.Claude, "CLAUDE.md"), "old global\n")

	if r := run(t, s, stack.Link); r.Conflicts() != 0 {
		t.Fatalf("conflicts on a fresh home: %+v", r.Rows)
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
	if got := read(t, filepath.Join(l.Claude, "CLAUDE.md.backup-20261116120000")); got != "old global\n" {
		t.Errorf("backup = %q", got)
	}

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
	if again.Conflicts() != 0 {
		t.Fatalf("second link has conflicts: %+v", again.Rows)
	}
	for _, row := range again.Rows {
		if row.State != "linked" && !strings.HasSuffix(row.State, "current") {
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
	st := states(run(t, s, stack.Status))
	if st["agent associate opencode"] != "on, current" || st["agent senior opencode"] != "off, current" {
		t.Errorf("status = %q / %q", st["agent associate opencode"], st["agent senior opencode"])
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

func TestConflictsAreLeftAlone(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	l := s.Layout
	foreignSkill := filepath.Join(l.AgentsSkills, "full-review", "SKILL.md")
	foreignAgent := filepath.Join(l.Claude, "agents", "associate.md")
	write(t, foreignSkill, "mine\n")
	write(t, foreignAgent, "my own associate agent\n")

	r := run(t, s, stack.Link)

	if r.Conflicts() != 2 {
		t.Errorf("conflicts = %d, want 2: %+v", r.Conflicts(), r.Rows)
	}
	if read(t, foreignSkill) != "mine\n" || read(t, foreignAgent) != "my own associate agent\n" {
		t.Error("a foreign file was changed")
	}
	if fm := frontmatter(t, filepath.Join(l.Claude, "agents", "senior.md")); fm["name"] != "senior" {
		t.Error("non-conflicting agents were not installed")
	}
}

func TestUnlinkRestoresHome(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	l := s.Layout
	write(t, filepath.Join(l.Claude, "CLAUDE.md"), "old global\n")
	write(t, filepath.Join(l.Claude, "agents", "mine.md"), "keep me\n")
	run(t, s, stack.Link)

	if r := run(t, s, stack.Unlink); r.Conflicts() != 0 {
		t.Errorf("unlink conflicts: %+v", r.Rows)
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
	claudeMD := filepath.Join(l.Claude, "CLAUDE.md")
	if fi, err := os.Lstat(claudeMD); err != nil || fi.Mode()&os.ModeSymlink != 0 || read(t, claudeMD) != "old global\n" {
		t.Error("CLAUDE.md was not restored from its backup")
	}
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
	run(t, s, stack.Link)

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

func TestRejectsDescriptionsThatBreakQuoting(t *testing.T) {
	src := fixtureSource(t)
	senior := filepath.Join(src, "agents", "senior.md")
	write(t, senior, strings.Replace(read(t, senior), "description: ", "description: a \"quoted\" ", 1))

	if _, err := newSyncer(t, src).Run(stack.Link); err == nil || !strings.Contains(err.Error(), "senior") {
		t.Errorf("err = %v, want one naming senior", err)
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
			out[row.Kind+" "+row.Name+" "+row.Harness] = row.Change
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
	if !slices.ContainsFunc(first.Rows, func(r stack.Row) bool { return r.Change == "backed up" && r.Harness == "claude" }) {
		t.Errorf("no backed-up row for claude: %+v", first.Rows)
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
	if got["skill keep claude"] != "removed" || got["agent senior opencode"] != "removed" || got["instructions AGENTS.md claude"] != "restored" || got["instructions AGENTS.md codex"] != "removed" {
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

	if read(t, codexMD) != "old codex global\n" {
		t.Error("codex AGENTS.md backup was not restored")
	}
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

func TestLinkDoesNotWriteThroughForeignAgentSymlink(t *testing.T) {
	s := newSyncer(t, repoRoot(t))
	target := filepath.Join(t.TempDir(), "elsewhere.md")
	link := filepath.Join(s.Layout.Claude, "agents", "senior.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	r := run(t, s, stack.Link)

	if _, err := os.Stat(target); err == nil {
		t.Errorf("rendered agent was written through a foreign symlink to %s", target)
	}
	if got, _ := os.Readlink(link); got != target {
		t.Errorf("foreign symlink was replaced: %q", got)
	}
	if !slices.ContainsFunc(r.Rows, func(row stack.Row) bool {
		return row.Conflict && row.Kind == "agent" && row.Name == "senior" && row.Harness == "claude"
	}) {
		t.Errorf("foreign symlink not reported as a conflict: %+v", r.Rows)
	}
}

func TestLinkDoesNotOverwriteUnreadableForeignAgent(t *testing.T) {
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

	if err := os.Chmod(foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, foreign); got != "my own senior\n" {
		t.Errorf("unreadable foreign agent was overwritten:\n%.80s", got)
	}
	if r.Conflicts() != 1 {
		t.Errorf("conflicts = %d, want 1: %+v", r.Conflicts(), r.Rows)
	}
}

func TestUnlinkWorksAfterTheSourceIsGone(t *testing.T) {
	src := fixtureSource(t, "keep")
	s := newSyncer(t, src)
	l := s.Layout
	run(t, s, stack.Link)
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}

	if r := run(t, s, stack.Unlink); r.Conflicts() != 0 {
		t.Errorf("unlink conflicts: %+v", r.Rows)
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

func TestUnlinkRestoresTheNewestBackupItMade(t *testing.T) {
	s := newSyncer(t, fixtureSource(t))
	claudeMD := filepath.Join(s.Layout.Claude, "CLAUDE.md")
	write(t, claudeMD+".backup-20260101000000", "older\n")
	write(t, claudeMD, "newest\n")
	run(t, s, stack.Link)
	write(t, claudeMD+".backup-old", "not ours\n")
	write(t, claudeMD+".backup-99999999999999", "not a time\n")

	run(t, s, stack.Unlink)

	if got := read(t, claudeMD); got != "newest\n" {
		t.Errorf("CLAUDE.md = %q, want the newest backup setup made", got)
	}
	if got := read(t, claudeMD+".backup-old"); got != "not ours\n" {
		t.Errorf("foreign backup changed: %q", got)
	}
	if got := read(t, claudeMD+".backup-20260101000000"); got != "older\n" {
		t.Errorf("older backup changed: %q", got)
	}
}
