package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

func newExtraUpstream(t *testing.T, skillPaths ...string) string {
	t.Helper()
	up := resolved(t, t.TempDir())
	gitT(t, "init", "--quiet", up)
	commitSkills(t, up, skillPaths...)
	return up
}

func commitSkills(t *testing.T, up string, skillPaths ...string) {
	t.Helper()
	for _, p := range skillPaths {
		writeFile(t, filepath.Join(up, p, "SKILL.md"), "---\nname: "+filepath.Base(p)+"\ndescription: test\n---\n")
	}
	gitT(t, "-C", up, "add", "-A")
	gitT(t, "-C", up, "commit", "--quiet", "-m", "skills")
}

type manifestEntry struct {
	Repo   string   `json:"repo"`
	Branch string   `json:"branch,omitempty"`
	Skills []string `json:"skills"`
}

func (f fixture) publishExtras(extras map[string]manifestEntry) {
	f.t.Helper()
	raw, err := json.Marshal(extras)
	if err != nil {
		f.t.Fatal(err)
	}
	f.commit(func(dir string) { writeFile(f.t, filepath.Join(dir, "extras.json"), string(raw)) })
}

func (f fixture) extraClone(name string) string { return filepath.Join(f.env.dataDir, "extras", name) }

func (f fixture) claudeSkill(name string) string {
	return filepath.Join(f.env.layout.Claude, "skills", name)
}

func TestInstallExtrasClonesAndLinksTheChosenSkills(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/engineering/tdd", "skills/misc/unwanted")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})

	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	if got := linkTarget(t, f.claudeSkill("tdd")); got != filepath.Join(f.extraClone("mp"), "skills", "engineering", "tdd") {
		t.Errorf("tdd links to %s", got)
	}
	if _, err := os.Lstat(f.claudeSkill("unwanted")); err == nil {
		t.Error("a skill the manifest does not list was linked")
	}
	if got := f.state().Extras; !slices.Equal(got, []string{"mp"}) {
		t.Errorf("state extras = %v", got)
	}
}

func TestInstallPullsTheChosenExtras(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	commitSkills(t, up, "skills/grilling")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd", "grilling"}}})
	f.mustRun("install")

	if got := linkTarget(t, f.claudeSkill("grilling")); got != filepath.Join(f.extraClone("mp"), "skills", "grilling") {
		t.Errorf("grilling links to %s", got)
	}
}

func TestInstallExtrasNoneRemovesTheLinksAndTheClone(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	f.mustRun("install", "--extras", "none")

	if _, err := os.Lstat(f.claudeSkill("tdd")); err == nil {
		t.Error("the extra's skill is still linked")
	}
	if _, err := os.Stat(f.extraClone("mp")); err == nil {
		t.Error("the extra's clone is still there")
	}
	if got := f.state().Extras; len(got) != 0 {
		t.Errorf("state extras = %v", got)
	}
	if _, err := os.Lstat(f.claudeSkill("full-review")); err != nil {
		t.Error("dropping extras removed a stack skill")
	}
}

func TestInstallRejectsAnUnknownExtra(t *testing.T) {
	f := newFixture(t)
	out, code := f.run("install", "--harness", "claude", "--extras", "nope")
	if code != 2 || !strings.Contains(out, `unknown extra "nope"`) {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestInstallReclonesAnExtraWhoseBranchChanged(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	gitT(t, "-C", up, "checkout", "--quiet", "-b", "next")
	commitSkills(t, up, "skills/fresh")
	gitT(t, "-C", up, "checkout", "--quiet", "-")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Branch: "next", Skills: []string{"fresh"}}})
	f.mustRun("install")

	if got := linkTarget(t, f.claudeSkill("fresh")); got != filepath.Join(f.extraClone("mp"), "skills", "fresh") {
		t.Errorf("fresh links to %s", got)
	}
	repo := source.Repo{Dir: f.extraClone("mp"), URL: up, Branch: "next", Git: source.ExecGit}
	if ok, err := repo.Tracks(context.Background()); err != nil || !ok {
		t.Errorf("the clone does not track the new branch: %v", err)
	}
}

func TestInstallRefusesToWipeAnExtraCloneWithLocalWork(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")
	gitT(t, "-C", f.extraClone("mp"), "checkout", "--quiet", "-b", "mine")
	mine := filepath.Join(f.extraClone("mp"), "skills", "tdd", "notes.md")
	writeFile(t, mine, "local work\n")

	out, code := f.run("install")

	if _, err := os.Stat(mine); err != nil {
		t.Error("install deleted local work in the extra's clone")
	}
	if code == 0 || !strings.Contains(out, "push or discard") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestInstallWithoutFlagOrTerminalKeepsTheLastExtras(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}, "other": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	f.mustRun("install")

	if got := f.state().Extras; !slices.Equal(got, []string{"mp"}) {
		t.Errorf("state extras = %v", got)
	}
}

func TestInstallAsksWhichExtrasInATerminal(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd", "skills/grilling")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Branch: "main", Skills: []string{"tdd", "grilling"}}})
	var offered []ui.Choice
	f.env.choose = func(_ context.Context, p ui.Pick) ([]string, error) {
		if !strings.Contains(p.Title, "extras") {
			return []string{"claude"}, nil
		}
		if !p.AllowEmpty {
			t.Error("the extras picker must allow choosing none")
		}
		offered = p.Choices
		return []string{"mp"}, nil
	}

	f.mustRun("install")

	if len(offered) != 1 {
		t.Fatalf("offered %+v", offered)
	}
	if c := offered[0]; c.Key != "mp" || c.On || c.Was || c.Detail != up+"@main" || c.Hint != "2 skills" {
		t.Errorf("first install offered %+v; an extra should start off", c)
	}
	if _, err := os.Lstat(f.claudeSkill("tdd")); err != nil {
		t.Error("the chosen extra was not linked")
	}

	f.mustRun("install")
	if c := offered[0]; !c.On || !c.Was {
		t.Errorf("rerun should start from the last choice, got %+v", c)
	}
}

func TestStatusListsTheChosenExtras(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	out := f.mustRun("status")

	if !strings.Contains(out, "Extras") || !strings.Contains(out, "mp") || !strings.Contains(out, "tdd") {
		t.Errorf("status:\n%s", out)
	}
}

func TestInstallRemovesADroppedCloneEvenWhenAnExtraSkillFails(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}, "bad": {Repo: up, Skills: []string{"absent"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")

	out, code := f.run("install", "--extras", "bad")

	if code == 0 {
		t.Errorf("a missing extra skill should fail the install:\n%s", out)
	}
	if _, err := os.Stat(f.extraClone("mp")); err == nil {
		t.Error("dropped clone kept")
	}
}

func TestInstallCanceledAtTheExtrasPickerChangesNothing(t *testing.T) {
	f := newFixture(t)
	up := newExtraUpstream(t, "skills/tdd")
	f.publishExtras(map[string]manifestEntry{"mp": {Repo: up, Skills: []string{"tdd"}}})
	f.mustRun("install", "--harness", "claude", "--extras", "mp")
	f.env.choose = func(_ context.Context, p ui.Pick) ([]string, error) {
		if strings.Contains(p.Title, "extras") {
			return nil, ui.ErrCanceled
		}
		return []string{"claude"}, nil
	}

	out, code := f.run("install")

	if code == 0 || !strings.Contains(out, "canceled") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if _, err := os.Lstat(f.claudeSkill("tdd")); err != nil {
		t.Error("cancel changed links")
	}
	if _, err := os.Stat(f.extraClone("mp")); err != nil {
		t.Error("cancel deleted the clone")
	}
}
