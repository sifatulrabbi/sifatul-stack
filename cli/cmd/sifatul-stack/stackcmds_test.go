package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

type fixture struct {
	t        *testing.T
	upstream string
	env      stackEnv
}

func gitT(t *testing.T, args ...string) {
	t.Helper()
	base := []string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}
	if _, err := source.ExecGit(context.Background(), append(base, args...)...); err != nil {
		t.Fatal(err)
	}
}

func resolved(t *testing.T, dir string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, to, string(raw))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	up := resolved(t, t.TempDir())
	files := []string{"agents/models.tsv", "agents/shared.md", "instructions/AGENTS.md", "skills/full-review/SKILL.md"}
	for _, tier := range []string{"principal", "senior", "junior", "associate"} {
		files = append(files, "agents/"+tier+".md")
	}
	for _, f := range files {
		copyFile(t, filepath.Join(root, f), filepath.Join(up, f))
	}
	gitT(t, "init", "--quiet", up)
	gitT(t, "-C", up, "add", "-A")
	gitT(t, "-C", up, "commit", "--quiet", "-m", "stack")

	home := resolved(t, t.TempDir())
	return fixture{t: t, upstream: up, env: stackEnv{
		layout:  stack.LayoutFromEnv(home, func(string) string { return "" }),
		dataDir: filepath.Join(home, ".local", "share", "sifatul-stack"),
		repoURL: up,
		git:     source.ExecGit,
		home:    home,
	}}
}

func (f fixture) clone() string { return filepath.Join(f.env.dataDir, "stack") }

func (f fixture) run(args ...string) (string, int) {
	f.t.Helper()
	return f.runContext(context.Background(), args...)
}

func (f fixture) runContext(ctx context.Context, args ...string) (string, int) {
	f.t.Helper()
	cmd := newRootCmd(app{stack: f.env})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	code := finish(&out, cmd.ExecuteContext(ctx))
	return out.String(), code
}

func (f fixture) mustRun(args ...string) string {
	f.t.Helper()
	out, code := f.run(args...)
	if code != 0 {
		f.t.Fatalf("%v exited %d:\n%s", args, code, out)
	}
	return out
}

func (f fixture) commit(edit func(dir string)) {
	f.t.Helper()
	edit(f.upstream)
	gitT(f.t, "-C", f.upstream, "add", "-A")
	gitT(f.t, "-C", f.upstream, "commit", "--quiet", "-m", "change")
}

func linkTarget(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return target
}

func TestInstallClonesAndLinks(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("install")

	if !strings.Contains(out, "sifatul-stack · cloned "+f.upstream) {
		t.Errorf("output lacks the clone line:\n%s", out)
	}
	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.clone(), "skills", "full-review") {
		t.Errorf("skill links to %s", got)
	}
	if _, err := os.Stat(filepath.Join(f.env.layout.Codex, "agents", "senior.toml")); err != nil {
		t.Error("agents were not rendered")
	}
	st, err := source.LoadState(f.env.statePath())
	if err != nil || st.Source != f.clone() || !st.Managed || !slices.Equal(st.Harnesses, stack.Harnesses) {
		t.Errorf("state = %+v %v", st, err)
	}
	if out := f.mustRun("install"); !strings.Contains(out, "sifatul-stack · already up to date") {
		t.Errorf("second install should pull the existing clone:\n%s", out)
	}
}

func TestInstallPullsTheLatestAndResyncs(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	f.commit(func(dir string) {
		writeFile(t, filepath.Join(dir, "skills", "fresh", "SKILL.md"), "---\nname: fresh\ndescription: new\n---\n")
		if err := os.RemoveAll(filepath.Join(dir, "skills", "full-review")); err != nil {
			t.Fatal(err)
		}
		tsv := filepath.Join(dir, "agents", "models.tsv")
		writeFile(t, tsv, strings.Replace(readFile(t, tsv), "senior\tclaude\topus\t", "senior\tclaude\topus-next\t", 1))
	})

	out := f.mustRun("install")

	if !strings.Contains(out, "sifatul-stack · updated ") {
		t.Errorf("output lacks the pull line:\n%s", out)
	}
	if got := linkTarget(t, filepath.Join(f.env.layout.AgentsSkills, "fresh")); got != filepath.Join(f.clone(), "skills", "fresh") {
		t.Errorf("new skill links to %s", got)
	}
	if _, err := os.Lstat(filepath.Join(f.env.layout.Claude, "skills", "full-review")); err == nil {
		t.Error("the removed skill is still linked")
	}
	if !strings.Contains(readFile(t, filepath.Join(f.env.layout.Claude, "agents", "senior.md")), "\nmodel: opus-next\n") {
		t.Error("senior was not re-rendered")
	}
	if out := f.mustRun("install"); !strings.Contains(out, "sifatul-stack · already up to date") {
		t.Errorf("second install:\n%s", out)
	}
}

func TestInstallRefusesADirtyClone(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	writeFile(t, filepath.Join(f.clone(), "agents", "shared.md"), "local edit\n")

	out, code := f.run("install")
	if code != 1 || !strings.Contains(out, "local changes") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestStatusBeforeInstallPointsToInstall(t *testing.T) {
	f := newFixture(t)
	if out := f.mustRun("status"); !strings.Contains(out, "not installed; run `sifatul-stack install`") {
		t.Errorf("status before install:\n%s", out)
	}
}

func TestOnlyInstallAndStatusManageTheStack(t *testing.T) {
	f := newFixture(t)
	for _, gone := range []string{"setup", "update", "remove"} {
		if _, code := f.run(gone); code != 2 {
			t.Errorf("%s exited %d, want 2 (unknown command)", gone, code)
		}
	}
}

func TestInstallFromCheckoutLinksItAndLeavesPullingToYou(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("install", "--from", f.upstream)

	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.upstream, "skills", "full-review") {
		t.Errorf("skill links to %s", got)
	}
	if _, err := os.Stat(f.clone()); err == nil {
		t.Error("install --from cloned anyway")
	}
	if !strings.Contains(out, "pull it yourself") {
		t.Errorf("install from a checkout:\n%s", out)
	}
}

func TestInstallSwitchesSources(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--from", f.upstream)
	out := f.mustRun("install")

	if !strings.Contains(out, "sifatul-stack · switching from "+f.upstream) {
		t.Errorf("output lacks the switch line:\n%s", out)
	}
	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.clone(), "skills", "full-review") {
		t.Errorf("skill links to %s after the switch", got)
	}
}

func noBackups(t *testing.T, root string) {
	t.Helper()
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), "backup") {
			t.Errorf("found a backup at %s", path)
		}
		return nil
	})
}

func TestDroppingAHarnessRestoresNothing(t *testing.T) {
	f := newFixture(t)
	claudeMD := filepath.Join(f.env.layout.Claude, "CLAUDE.md")
	writeFile(t, claudeMD, "old global\n")
	f.mustRun("install")

	f.mustRun("install", "--harness", "codex")

	if _, err := os.Lstat(claudeMD); err == nil {
		t.Error("CLAUDE.md is back after dropping Claude Code")
	}
	noBackups(t, f.env.home)
	if got := f.state().Harnesses; !slices.Equal(got, []string{"codex"}) {
		t.Errorf("state harnesses = %v", got)
	}
}

func TestInstallReportsFailuresWithExitOne(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes everywhere")
	}
	f := newFixture(t)
	dir := filepath.Join(f.env.layout.Claude, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	out, code := f.run("install", "--harness", "claude")
	if code != 1 || !strings.Contains(out, "! 4 failed") || !strings.Contains(out, "agent senior · Claude Code") || !strings.Contains(out, "fix the errors above and rerun") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestInstallReplacesWhateverSitsAtATargetPath(t *testing.T) {
	plants := map[string]func(t *testing.T, path string){
		"file": func(t *testing.T, path string) { writeFile(t, path, "mine\n") },
		"dir":  func(t *testing.T, path string) { writeFile(t, filepath.Join(path, "inside"), "mine\n") },
		"symlink": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "theirs")
			writeFile(t, target, "theirs\n")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for what, plant := range plants {
		t.Run(what, func(t *testing.T) {
			f := newFixture(t)
			l := f.env.layout
			skill := filepath.Join(l.Claude, "skills", "full-review")
			instructions := filepath.Join(l.Codex, "AGENTS.md")
			agent := filepath.Join(l.OpenCode, "agents", "senior.md")
			for _, p := range []string{skill, instructions, agent} {
				plant(t, p)
			}

			f.mustRun("install")

			if got := linkTarget(t, skill); got != filepath.Join(f.clone(), "skills", "full-review") {
				t.Errorf("skill links to %s", got)
			}
			if got := linkTarget(t, instructions); got != filepath.Join(f.clone(), "instructions", "AGENTS.md") {
				t.Errorf("instructions link to %s", got)
			}
			if fi, err := os.Lstat(agent); err != nil || !fi.Mode().IsRegular() || !strings.Contains(readFile(t, agent), "mode: subagent") {
				t.Errorf("senior.md was not rendered over the %s", what)
			}
			noBackups(t, f.env.home)
		})
	}
}

func TestStatusReportsAForeignThingWithoutChangingIt(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	codexMD := filepath.Join(f.env.layout.Codex, "AGENTS.md")
	if err := os.Remove(codexMD); err != nil {
		t.Fatal(err)
	}
	writeFile(t, codexMD, "mine\n")

	out := f.mustRun("status")

	if !strings.Contains(out, "~ stale") || !strings.Contains(out, "run `sifatul-stack install`") {
		t.Errorf("status:\n%s", out)
	}
	if readFile(t, codexMD) != "mine\n" {
		t.Error("status changed the foreign file")
	}
}

var allTiers = []string{"principal", "senior", "junior", "associate"}

func (f fixture) state() source.State {
	f.t.Helper()
	st, err := source.LoadState(f.env.statePath())
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func dropTier(t *testing.T, dir, tier string) {
	t.Helper()
	tsv := filepath.Join(dir, "agents", "models.tsv")
	var kept []string
	for _, line := range strings.Split(readFile(t, tsv), "\n") {
		if !strings.HasPrefix(line, tier+"\t") {
			kept = append(kept, line)
		}
	}
	writeFile(t, tsv, strings.Join(kept, "\n"))
}

func TestStateRecordsTheRenderedAgents(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	if got := f.state().Agents; !slices.Equal(got, allTiers) {
		t.Errorf("after the first install agents = %v, want %v", got, allTiers)
	}
	f.commit(func(dir string) { dropTier(t, dir, "associate") })

	f.mustRun("install")

	if got := f.state().Agents; !slices.Equal(got, allTiers[:3]) {
		t.Errorf("after the second install agents = %v, want %v", got, allTiers[:3])
	}
}

func TestInstallPrunesATierDroppedFromModelsTSV(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	mine := filepath.Join(f.env.layout.Codex, "agents", "my-helper.toml")
	writeFile(t, mine, "name = \"my-helper\"\n")
	f.commit(func(dir string) { dropTier(t, dir, "associate") })

	out := f.mustRun("install")

	for _, p := range []string{
		filepath.Join(f.env.layout.Claude, "agents", "associate.md"),
		filepath.Join(f.env.layout.Codex, "agents", "associate.toml"),
		filepath.Join(f.env.layout.OpenCode, "agents", "associate.md"),
	} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s survived the install", p)
		}
	}
	if !strings.Contains(out, "− pruned  associate") {
		t.Errorf("install output lacks the prune:\n%s", out)
	}
	if readFile(t, mine) != "name = \"my-helper\"\n" {
		t.Error("a user agent with an unrelated name was changed")
	}
}

func TestDroppingHarnessesDeletesOnlyTheAgentsItOwns(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	l := f.env.layout
	copies := map[string]string{
		filepath.Join(l.Claude, "agents", "senior.md"):   filepath.Join(l.Claude, "agents", "my-reviewer.md"),
		filepath.Join(l.OpenCode, "agents", "senior.md"): filepath.Join(l.OpenCode, "agents", "my-reviewer.md"),
	}
	for from, to := range copies {
		copyFile(t, from, to)
	}

	f.mustRun("install", "--harness", "codex")

	for from, to := range copies {
		if _, err := os.Lstat(from); err == nil {
			t.Errorf("%s survived dropping its harness", from)
		}
		if _, err := os.Stat(to); err != nil {
			t.Errorf("dropping a harness deleted the user's %s", to)
		}
	}
}

func TestDroppingAHarnessRemovesItsOwnedAgents(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--harness", "claude,codex")
	mine := filepath.Join(f.env.layout.Codex, "agents", "my-reviewer.toml")
	writeFile(t, mine, "name = \"my-reviewer\"\n")

	f.mustRun("install", "--harness", "claude")

	for _, tier := range allTiers {
		if _, err := os.Lstat(filepath.Join(f.env.layout.Codex, "agents", tier+".toml")); err == nil {
			t.Errorf("codex %s survived dropping codex", tier)
		}
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("dropping codex deleted the user's own agent")
	}
}

func TestNothingTouchesAHarnessNeverChosen(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--harness", "claude")
	mine := map[string]string{
		filepath.Join(f.env.layout.Codex, "agents", "senior.toml"):  "codex mine\n",
		filepath.Join(f.env.layout.Codex, "AGENTS.md"):              "codex global\n",
		filepath.Join(f.env.layout.OpenCode, "agents", "senior.md"): "opencode mine\n",
	}
	for path, text := range mine {
		writeFile(t, path, text)
	}

	for _, args := range [][]string{{"install", "--harness", "claude"}, {"install"}, {"status"}} {
		f.mustRun(args...)
		for path, text := range mine {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != text {
				t.Errorf("after %v, %s = %q, %v; want %q", args, path, got, err, text)
			}
		}
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	f := newFixture(t)
	if _, code := f.run("bogus"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

func (f fixture) installed(harness string) bool {
	path := map[string]string{
		"claude":   filepath.Join(f.env.layout.Claude, "agents", "senior.md"),
		"codex":    filepath.Join(f.env.layout.Codex, "agents", "senior.toml"),
		"opencode": filepath.Join(f.env.layout.OpenCode, "agents", "senior.md"),
	}[harness]
	_, err := os.Stat(path)
	return err == nil
}

func (f fixture) installedSet() []string {
	return slices.DeleteFunc(slices.Clone(stack.Harnesses), func(h string) bool { return !f.installed(h) })
}

func TestInstallHarnessFlagLimitsTheInstall(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("install", "--harness", "claude", "--harness", "opencode")

	if got := f.installedSet(); !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("installed into %v", got)
	}
	if !strings.Contains(out, "sifatul-stack · installing into Claude Code, OpenCode") {
		t.Errorf("output lacks the harness line:\n%s", out)
	}
	f.mustRun("install")
	if got := f.installedSet(); !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("a rerun widened the install to %v", got)
	}
	if out := f.mustRun("status"); !strings.Contains(out, "Harnesses      Claude Code, OpenCode") || strings.Contains(out, "Codex") || strings.Count(out, "✓ current") != 4 {
		t.Errorf("status should cover only the chosen harnesses:\n%s", out)
	}
}

func TestInstallDroppingAHarnessRemovesIt(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--harness", "claude,codex")
	f.mustRun("install", "--harness", "claude")

	if got := f.installedSet(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("installed into %v after dropping codex", got)
	}
}

func TestInstallWithoutFlagOrTerminalKeepsTheLastChoice(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--harness", "codex")
	f.mustRun("install")

	if got := f.installedSet(); !slices.Equal(got, []string{"codex"}) {
		t.Errorf("installed into %v", got)
	}
}

func TestInstallRejectsAnUnknownHarness(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("install", "--harness", "cursor")
	if code != 2 || !strings.Contains(out, "unknown harness") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(f.env.statePath()); err == nil {
		t.Error("a rejected install saved state")
	}
}

func TestInstallAsksWhichHarnessesInATerminal(t *testing.T) {
	f := newFixture(t)
	var offered []ui.Choice
	f.env.found = func(h stack.Harness) bool { return h.Key == "codex" }
	f.env.choose = func(_ context.Context, choices []ui.Choice) ([]string, error) {
		offered = choices
		return []string{"claude"}, nil
	}

	f.mustRun("install")

	if len(offered) != 3 {
		t.Fatalf("offered %+v", offered)
	}
	if c := offered[1]; c.Key != "codex" || !c.On || c.Hint != "found" || c.Was || c.Detail != "~/.codex" {
		t.Errorf("codex choice = %+v; a found harness should start on, with its tilde path", c)
	}
	if c := offered[0]; c.On || c.Hint != "not found" {
		t.Errorf("claude choice = %+v; a harness not found should start off", c)
	}
	if got := f.installedSet(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("installed into %v", got)
	}

	f.mustRun("install")
	if c := offered[0]; !c.On || !c.Was {
		t.Errorf("rerun should start from the last choice, got %+v", c)
	}
	if c := offered[1]; c.On {
		t.Errorf("codex was not chosen last time, got %+v", c)
	}
}

func TestInstallCanceledChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.env.choose = func(context.Context, []ui.Choice) ([]string, error) { return nil, ui.ErrCanceled }

	out, code := f.run("install")

	if code != 1 || !strings.Contains(out, "nothing changed") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if got := f.installedSet(); len(got) != 0 {
		t.Errorf("a canceled install installed into %v", got)
	}
	if _, err := os.Stat(f.clone()); err == nil {
		t.Error("a canceled install cloned anyway")
	}
}

func TestInstallFlagSkipsThePicker(t *testing.T) {
	f := newFixture(t)
	f.env.choose = func(context.Context, []ui.Choice) ([]string, error) {
		t.Error("the picker ran despite --harness")
		return nil, nil
	}
	f.mustRun("install", "--harness", "codex")
}

func TestInstallSurvivesAMovedCheckout(t *testing.T) {
	for name, next := range map[string][]string{
		"install":                      {"install"},
		"install from":                 {"install", "--from", ""},
		"install dropping Claude Code": {"install", "--from", "", "--harness", "codex"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			old := filepath.Join(resolved(t, t.TempDir()), "checkout")
			if err := os.Rename(f.upstream, old); err != nil {
				t.Fatal(err)
			}
			f.mustRun("install", "--from", old)
			moved := old + "-moved"
			if err := os.Rename(old, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, f.upstream); err != nil {
				t.Fatal(err)
			}
			args := slices.Clone(next)
			if i := slices.Index(args, ""); i >= 0 {
				args[i] = moved
			}
			if out, code := f.run(args...); code != 0 {
				t.Fatalf("%v exited %d:\n%s", args, code, out)
			}
			for _, link := range []string{
				filepath.Join(f.env.layout.Claude, "skills", "full-review"),
				filepath.Join(f.env.layout.Claude, "CLAUDE.md"),
			} {
				if target, err := os.Readlink(link); err == nil && strings.HasPrefix(target, old+string(filepath.Separator)) {
					t.Errorf("%s still points into the moved checkout: %s", link, target)
				}
			}
		})
	}
}

func TestSwitchingSourcesKeepsOpenCodeChoices(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--from", f.upstream)
	senior := filepath.Join(f.env.layout.OpenCode, "agents", "senior.md")
	writeFile(t, senior, strings.Replace(readFile(t, senior), "disabled: true\n", "", 1))

	f.mustRun("install")

	if strings.Contains(readFile(t, senior), "disabled: true\n") {
		t.Error("switching sources switched senior back off in OpenCode")
	}
	if !strings.Contains(readFile(t, filepath.Join(f.env.layout.OpenCode, "agents", "junior.md")), "disabled: true\n") {
		t.Error("switching sources switched junior on in OpenCode")
	}
}

func TestStaleAgentsShowAsStaleThenReRenderedThroughTheRealSyncer(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	for _, p := range []string{
		filepath.Join(f.env.layout.Claude, "agents", "senior.md"),
		filepath.Join(f.env.layout.OpenCode, "agents", "senior.md"),
	} {
		writeFile(t, p, readFile(t, p)+"local drift\n")
	}

	status := f.mustRun("status")
	for _, want := range []string{"~ stale", "run `sifatul-stack install`"} {
		if !strings.Contains(status, want) {
			t.Errorf("status lacks %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "✓ stale") || strings.Contains(status, "✓ off") {
		t.Errorf("status shows a stale or off agent as healthy:\n%s", status)
	}

	install := f.mustRun("install")
	for _, want := range []string{"~ re-rendered  senior (Claude Code, OpenCode)", "4 off in OpenCode"} {
		if !strings.Contains(install, want) {
			t.Errorf("install lacks %q:\n%s", want, install)
		}
	}
}

func TestInterruptedPullStopsBeforeSyncingAndExits130(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install")
	ctx, interrupt := context.WithCancelCause(context.Background())
	f.env.git = func(gctx context.Context, args ...string) ([]byte, error) {
		if slices.Contains(args, "pull") {
			interrupt(ui.ErrInterrupted)
			<-gctx.Done()
			return nil, gctx.Err()
		}
		return source.ExecGit(gctx, args...)
	}
	out, code := f.runContext(ctx, "install")
	if code != 130 || strings.Contains(out, "✗") || strings.Contains(out, "skills") {
		t.Fatalf("code = %d, out:\n%s", code, out)
	}
}

func TestInterruptedCloneSavesNoStateAndExits130(t *testing.T) {
	f := newFixture(t)
	ctx, interrupt := context.WithCancelCause(context.Background())
	f.env.git = func(gctx context.Context, args ...string) ([]byte, error) {
		if slices.Contains(args, "clone") {
			interrupt(ui.ErrInterrupted)
			<-gctx.Done()
			return nil, gctx.Err()
		}
		return source.ExecGit(gctx, args...)
	}
	out, code := f.runContext(ctx, "install")
	if code != 130 || strings.Contains(out, "✗") {
		t.Fatalf("code = %d, out:\n%s", code, out)
	}
	if _, err := os.Stat(f.env.statePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state.json written after interrupt: %v", err)
	}
	if f.installed("claude") {
		t.Fatal("install linked claude after interrupt")
	}
}

func TestInstallSwitchesSourcesPastAnUnknownSavedHarness(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--from", f.upstream, "--harness", "claude,opencode")
	writeFile(t, f.env.statePath(), strings.Replace(readFile(t, f.env.statePath()), `"opencode"`, `"gemini"`, 1))

	out, code := f.run("install", "--harness", "claude")

	if code != 0 {
		t.Fatalf("install switch exited %d:\n%s", code, out)
	}
	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.clone(), "skills", "full-review") {
		t.Errorf("skill links to %s", got)
	}
}

func TestAnUnknownSavedHarnessErrorNamesTheStateFile(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--from", f.upstream, "--harness", "claude,opencode")
	writeFile(t, f.env.statePath(), strings.Replace(readFile(t, f.env.statePath()), `"opencode"`, `"gemini"`, 1))

	for _, args := range [][]string{{"status"}, {"install", "--from", f.upstream}} {
		out, code := f.run(args...)
		if code != 1 || strings.Contains(out, "stack: ") || !strings.Contains(out, f.env.statePath()+`: unknown harness "gemini"`) {
			t.Errorf("%v exited %d:\n%s", args, code, out)
		}
	}
}

func TestInstallWritesThroughASymlinkedState(t *testing.T) {
	f := newFixture(t)
	f.mustRun("install", "--from", f.upstream)
	dot := filepath.Join(f.env.home, "dot", "state.json")
	if err := os.MkdirAll(filepath.Dir(dot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.env.statePath(), dot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dot, f.env.statePath()); err != nil {
		t.Fatal(err)
	}

	f.mustRun("install")

	if got := linkTarget(t, f.env.statePath()); got != dot {
		t.Errorf("state.json now links to %q, want %q", got, dot)
	}
	if st, err := source.LoadState(dot); err != nil || st.Source != f.clone() {
		t.Errorf("symlink target holds %+v %v, want source %s", st, err, f.clone())
	}
}
