package stack_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
)

func TestLoadExtrasReadsTheManifestByName(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "extras.json"), `{
  "zeta": {"repo": "https://example.com/zeta", "skills": ["z"]},
  "mattpocock": {"repo": "https://github.com/mattpocock/skills", "branch": "main", "skills": ["tdd", "grilling"]}
}`)

	got, err := stack.LoadExtras(src)

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "mattpocock" || got[1].Name != "zeta" {
		t.Fatalf("extras = %+v, want mattpocock then zeta", got)
	}
	if m := got[0]; m.Repo != "https://github.com/mattpocock/skills" || m.Branch != "main" || !slices.Equal(m.Skills, []string{"tdd", "grilling"}) {
		t.Errorf("mattpocock = %+v", m)
	}
}

func TestLoadExtrasWithoutAManifestIsEmpty(t *testing.T) {
	got, err := stack.LoadExtras(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Errorf("extras = %+v, err = %v", got, err)
	}
}

func TestLoadExtrasRejectsABadManifest(t *testing.T) {
	for name, manifest := range map[string]string{
		"unknown key":     `{"x": {"repo": "r", "skills": ["a"], "branche": "main"}}`,
		"no repo":         `{"x": {"skills": ["a"]}}`,
		"no skills":       `{"x": {"repo": "r", "skills": []}}`,
		"path as name":    `{"../x": {"repo": "r", "skills": ["a"]}}`,
		"path as skill":   `{"x": {"repo": "r", "skills": ["../a"]}}`,
		"flag as branch":  `{"x": {"repo": "r", "branch": "-x", "skills": ["a"]}}`,
		"repeated skill":  `{"x": {"repo": "r", "skills": ["a", "a"]}}`,
		"not json":        `{"x": `,
		"trailing object": `{"x": {"repo": "r", "skills": ["a"]}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			write(t, filepath.Join(src, "extras.json"), manifest)
			if _, err := stack.LoadExtras(src); err == nil || !strings.Contains(err.Error(), "extras.json") {
				t.Errorf("err = %v, want one naming extras.json", err)
			}
		})
	}
}

func plantExtra(t *testing.T, root, name string, skillPaths ...string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	for _, p := range skillPaths {
		write(t, filepath.Join(dir, p, "SKILL.md"), "---\nname: "+filepath.Base(p)+"\ndescription: test\n---\n")
	}
	return dir
}

func extrasSyncer(t *testing.T, stackSkills ...string) (stack.Syncer, string) {
	t.Helper()
	s := newSyncer(t, fixtureSource(t, stackSkills...))
	s.ExtrasDir = t.TempDir()
	return s, s.ExtrasDir
}

func TestExtraSkillsLinkByFolderNameWhereverTheyLive(t *testing.T) {
	s, root := extrasSyncer(t, "full-review")
	dir := plantExtra(t, root, "mp", "skills/engineering/tdd", "skills/productivity/grilling", "skills/misc/unwanted")
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd", "grilling"}}}

	st := states(run(t, s, stack.Link))

	for _, g := range []string{filepath.Join(s.Layout.Claude, "skills"), s.Layout.AgentsSkills} {
		assertLinksTo(t, filepath.Join(g, "tdd"), filepath.Join(dir, "skills", "engineering", "tdd"))
		assertLinksTo(t, filepath.Join(g, "grilling"), filepath.Join(dir, "skills", "productivity", "grilling"))
		assertMissing(t, filepath.Join(g, "unwanted"))
		assertLinksTo(t, filepath.Join(g, "full-review"), filepath.Join(s.Source, "skills", "full-review"))
	}
	if st["skill tdd claude"] != "linked" || st["skill grilling agents"] != "linked" {
		t.Errorf("rows = %v", st)
	}
	if st := states(run(t, s, stack.Status)); st["skill tdd claude"] != "linked" {
		t.Errorf("status = %v", st)
	}
}

func TestExtraSkillsSkipTheCloneMetadata(t *testing.T) {
	s, root := extrasSyncer(t)
	dir := plantExtra(t, root, "mp", ".git/hooks/tdd", "node_modules/pkg/tdd", "skills/tdd")
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd"}}}

	st := states(run(t, s, stack.Link))

	if st["skill tdd claude"] != "linked" {
		t.Fatalf("rows = %v", st)
	}
	assertLinksTo(t, filepath.Join(s.Layout.Claude, "skills", "tdd"), filepath.Join(dir, "skills", "tdd"))
}

func failures(r stack.Report) map[string]string {
	out := map[string]string{}
	for _, row := range r.Rows {
		if row.State == stack.Failed {
			out[row.Name+" "+row.Harness] = row.Detail
		}
	}
	return out
}

func TestAnExtraSkillThatCannotBePlacedFailsAlone(t *testing.T) {
	s, root := extrasSyncer(t, "full-review")
	plantExtra(t, root, "mp", "skills/a/twice", "skills/b/twice", "skills/ok", "skills/full-review")
	plantExtra(t, root, "other", "ok")
	s.Extras = []stack.Extra{
		{Name: "mp", Skills: []string{"absent", "twice", "ok", "full-review"}},
		{Name: "other", Skills: []string{"ok"}},
	}

	r := run(t, s, stack.Link)
	bad := failures(r)

	for name, want := range map[string]string{
		"absent":      "no absent/SKILL.md in extra mp",
		"twice":       "skills/a/twice, skills/b/twice",
		"full-review": "the stack has its own full-review",
		"ok":          "extra mp already links ok",
	} {
		for _, g := range []string{"claude", "agents"} {
			if !strings.Contains(bad[name+" "+g], want) {
				t.Errorf("%s on %s failed with %q, want it to say %q", name, g, bad[name+" "+g], want)
			}
		}
	}
	if len(bad) != 8 {
		t.Errorf("failures = %v", bad)
	}
	assertLinksTo(t, filepath.Join(s.Layout.Claude, "skills", "ok"), filepath.Join(root, "mp", "skills", "ok"))
	assertLinksTo(t, filepath.Join(s.Layout.Claude, "skills", "full-review"), filepath.Join(s.Source, "skills", "full-review"))
}

func TestAnExtraNotClonedIsMissingInStatusAndFailsALink(t *testing.T) {
	s, _ := extrasSyncer(t)
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd"}}}

	if st := states(run(t, s, stack.Status)); st["skill tdd claude"] != "missing" {
		t.Errorf("status = %v", st)
	}
	if bad := failures(run(t, s, stack.Link)); !strings.Contains(bad["tdd claude"], "extra mp is not cloned") {
		t.Errorf("failures = %v", bad)
	}
}

func TestDroppedExtraSkillsArePruned(t *testing.T) {
	s, root := extrasSyncer(t, "full-review")
	plantExtra(t, root, "mp", "skills/tdd", "skills/grilling")
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd", "grilling"}}}
	run(t, s, stack.Link)

	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd"}}}
	if st := states(run(t, s, stack.Status)); st["skill grilling claude"] != "dangling" {
		t.Errorf("status = %v", st)
	}
	st := states(run(t, s, stack.Link))
	if st["skill grilling claude"] != "pruned" {
		t.Errorf("rows = %v", st)
	}
	assertMissing(t, filepath.Join(s.Layout.Claude, "skills", "grilling"))

	s.Extras = nil
	run(t, s, stack.Link)
	assertMissing(t, filepath.Join(s.Layout.AgentsSkills, "tdd"))
	assertLinksTo(t, filepath.Join(s.Layout.Claude, "skills", "full-review"), filepath.Join(s.Source, "skills", "full-review"))
}

func TestExtraSkillReplacesWhateverSitsAtItsPath(t *testing.T) {
	s, root := extrasSyncer(t)
	dir := plantExtra(t, root, "mp", "skills/teach")
	mine := filepath.Join(s.Layout.Claude, "skills", "teach")
	plantDir(t, mine)
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"teach"}}}

	run(t, s, stack.Link)

	assertLinksTo(t, mine, filepath.Join(dir, "skills", "teach"))
}

func TestUnlinkRemovesExtraSkills(t *testing.T) {
	s, root := extrasSyncer(t)
	plantExtra(t, root, "mp", "skills/tdd")
	s.Extras = []stack.Extra{{Name: "mp", Skills: []string{"tdd"}}}
	run(t, s, stack.Link)

	s.Extras = nil
	run(t, s, stack.Unlink)

	assertMissing(t, filepath.Join(s.Layout.Claude, "skills", "tdd"))
	if _, err := os.Stat(filepath.Join(root, "mp", "skills", "tdd")); err != nil {
		t.Error("unlink touched the extra's clone")
	}
}
