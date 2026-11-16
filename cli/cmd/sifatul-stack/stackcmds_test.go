package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
		now:     func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) },
		home:    home,
	}}
}

func (f fixture) clone() string { return filepath.Join(f.env.dataDir, "stack") }

func (f fixture) run(args ...string) (string, int) {
	f.t.Helper()
	cmd := newRootCmd(app{stack: f.env})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		out.WriteString("Error: " + err.Error() + "\n")
	}
	return out.String(), exitCode(err)
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

func TestSetupClonesAndLinks(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("setup")

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
	if out := f.mustRun("setup"); !strings.Contains(out, "sifatul-stack · using the clone at ~/.local/share/sifatul-stack/stack") {
		t.Errorf("second setup should reuse the clone:\n%s", out)
	}
}

func TestUpdatePullsAndResyncs(t *testing.T) {
	f := newFixture(t)
	f.mustRun("setup")
	f.commit(func(dir string) {
		writeFile(t, filepath.Join(dir, "skills", "fresh", "SKILL.md"), "---\nname: fresh\ndescription: new\n---\n")
		if err := os.RemoveAll(filepath.Join(dir, "skills", "full-review")); err != nil {
			t.Fatal(err)
		}
		tsv := filepath.Join(dir, "agents", "models.tsv")
		writeFile(t, tsv, strings.Replace(readFile(t, tsv), "senior\tclaude\topus\t", "senior\tclaude\topus-next\t", 1))
	})

	out := f.mustRun("update")

	if !strings.Contains(out, "sifatul-stack · updated ") {
		t.Errorf("output lacks the update line:\n%s", out)
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
	if out := f.mustRun("update"); !strings.Contains(out, "sifatul-stack · already up to date") {
		t.Errorf("second update:\n%s", out)
	}
}

func TestUpdateRefusesADirtyClone(t *testing.T) {
	f := newFixture(t)
	f.mustRun("setup")
	writeFile(t, filepath.Join(f.clone(), "agents", "shared.md"), "local edit\n")

	out, code := f.run("update")
	if code != 1 || !strings.Contains(out, "local changes") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestUpdateBeforeSetupPointsToSetup(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("update")
	if code != 1 || !strings.Contains(out, "sifatul-stack setup") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if out := f.mustRun("status"); !strings.Contains(out, "not set up") {
		t.Errorf("status before setup:\n%s", out)
	}
}

func TestSetupFromCheckoutLinksItAndUpdateLeavesItAlone(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("setup", "--from", f.upstream)

	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.upstream, "skills", "full-review") {
		t.Errorf("skill links to %s", got)
	}
	if _, err := os.Stat(f.clone()); err == nil {
		t.Error("setup --from cloned anyway")
	}
	out = f.mustRun("update")
	if !strings.Contains(out, "pull it yourself") {
		t.Errorf("update on a checkout:\n%s", out)
	}
}

func TestSetupSwitchesSources(t *testing.T) {
	f := newFixture(t)
	f.mustRun("setup", "--from", f.upstream)
	out := f.mustRun("setup")

	if !strings.Contains(out, "sifatul-stack · switching from "+f.upstream) {
		t.Errorf("output lacks the switch line:\n%s", out)
	}
	if got := linkTarget(t, filepath.Join(f.env.layout.Claude, "skills", "full-review")); got != filepath.Join(f.clone(), "skills", "full-review") {
		t.Errorf("skill links to %s after the switch", got)
	}
}

func TestRemoveRestoresAndForgetsState(t *testing.T) {
	f := newFixture(t)
	claudeMD := filepath.Join(f.env.layout.Claude, "CLAUDE.md")
	writeFile(t, claudeMD, "old global\n")
	f.mustRun("setup")

	f.mustRun("remove")

	if fi, err := os.Lstat(claudeMD); err != nil || fi.Mode()&os.ModeSymlink != 0 || readFile(t, claudeMD) != "old global\n" {
		t.Error("CLAUDE.md was not restored")
	}
	if _, err := os.Stat(f.env.statePath()); err == nil {
		t.Error("state survived remove")
	}
	if _, err := os.Stat(f.clone()); err != nil {
		t.Error("remove deleted the clone")
	}
}

func TestSetupReportsConflictsWithExitOne(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.env.layout.Claude, "agents", "senior.md"), "mine\n")

	out, code := f.run("setup")
	if code != 1 || !strings.Contains(out, "! 1 conflict, left alone") || !strings.Contains(out, "agent senior · Claude Code  ~/.claude/agents/senior.md is not managed by sifatul-stack") {
		t.Errorf("exit %d:\n%s", code, out)
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

func TestSetupHarnessFlagLimitsTheInstall(t *testing.T) {
	f := newFixture(t)
	out := f.mustRun("setup", "--harness", "claude", "--harness", "opencode")

	if got := f.installedSet(); !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("installed into %v", got)
	}
	if !strings.Contains(out, "sifatul-stack · installing into Claude Code, OpenCode") {
		t.Errorf("output lacks the harness line:\n%s", out)
	}
	f.mustRun("update")
	if got := f.installedSet(); !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("update widened the install to %v", got)
	}
	if out := f.mustRun("status"); !strings.Contains(out, "Harnesses      Claude Code, OpenCode") || strings.Contains(out, "Codex") || strings.Count(out, "✓ current") != 4 {
		t.Errorf("status should cover only the chosen harnesses:\n%s", out)
	}
}

func TestSetupDroppingAHarnessRemovesIt(t *testing.T) {
	f := newFixture(t)
	f.mustRun("setup", "--harness", "claude,codex")
	f.mustRun("setup", "--harness", "claude")

	if got := f.installedSet(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("installed into %v after dropping codex", got)
	}
}

func TestSetupWithoutFlagOrTerminalKeepsTheLastChoice(t *testing.T) {
	f := newFixture(t)
	f.mustRun("setup", "--harness", "codex")
	f.mustRun("setup")

	if got := f.installedSet(); !slices.Equal(got, []string{"codex"}) {
		t.Errorf("installed into %v", got)
	}
}

func TestSetupRejectsAnUnknownHarness(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("setup", "--harness", "cursor")
	if code != 2 || !strings.Contains(out, "unknown harness") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(f.env.statePath()); err == nil {
		t.Error("a rejected setup saved state")
	}
}

func TestSetupAsksWhichHarnessesInATerminal(t *testing.T) {
	f := newFixture(t)
	var offered []ui.Choice
	f.env.found = func(h string) bool { return h == "codex" }
	f.env.choose = func(_ context.Context, choices []ui.Choice) ([]string, error) {
		offered = choices
		return []string{"claude"}, nil
	}

	f.mustRun("setup")

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

	f.mustRun("setup")
	if c := offered[0]; !c.On || !c.Was {
		t.Errorf("rerun should start from the last choice, got %+v", c)
	}
	if c := offered[1]; c.On {
		t.Errorf("codex was not chosen last time, got %+v", c)
	}
}

func TestSetupCanceledChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.env.choose = func(context.Context, []ui.Choice) ([]string, error) { return nil, ui.ErrCanceled }

	out, code := f.run("setup")

	if code != 1 || !strings.Contains(out, "nothing changed") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if got := f.installedSet(); len(got) != 0 {
		t.Errorf("a canceled setup installed into %v", got)
	}
	if _, err := os.Stat(f.clone()); err == nil {
		t.Error("a canceled setup cloned anyway")
	}
}

func TestSetupFlagSkipsThePicker(t *testing.T) {
	f := newFixture(t)
	f.env.choose = func(context.Context, []ui.Choice) ([]string, error) {
		t.Error("the picker ran despite --harness")
		return nil, nil
	}
	f.mustRun("setup", "--harness", "codex")
}
