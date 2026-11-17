package source_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/source"
)

type upstream struct {
	t   *testing.T
	dir string
}

func git(t *testing.T, args ...string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := []string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}
	out, err := source.ExecGit(context.Background(), append(base, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func newUpstream(t *testing.T) upstream {
	t.Helper()
	u := upstream{t: t, dir: t.TempDir()}
	git(t, "init", "--quiet", u.dir)
	u.commit("skills/a/SKILL.md", "a\n")
	return u
}

func (u upstream) commit(path, content string) {
	u.t.Helper()
	full := filepath.Join(u.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		u.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		u.t.Fatal(err)
	}
	git(u.t, "-C", u.dir, "add", "-A")
	git(u.t, "-C", u.dir, "commit", "--quiet", "-m", "change "+path)
}

func repo(t *testing.T, u upstream) source.Repo {
	return source.Repo{Dir: filepath.Join(t.TempDir(), "data", "stack"), URL: u.dir, Git: source.ExecGit}
}

func TestEnsureClonesOnceThenReuses(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()

	cloned, err := r.Ensure(ctx)
	if err != nil || !cloned {
		t.Fatalf("first ensure: cloned=%v err=%v", cloned, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "a", "SKILL.md")); err != nil {
		t.Fatal("clone lacks the upstream file")
	}
	cloned, err = r.Ensure(ctx)
	if err != nil || cloned {
		t.Fatalf("second ensure: cloned=%v err=%v", cloned, err)
	}
}

func TestEnsureRefusesADirThatIsNotAClone(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(r.Dir))
	if _, err := r.Ensure(context.Background()); err == nil {
		t.Fatal("ensure accepted a plain directory")
	}
}

func TestPullFastForwards(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	change, err := r.Pull(ctx)
	if err != nil || change.Updated() {
		t.Fatalf("pull with nothing new: %+v %v", change, err)
	}

	u.commit("skills/b/SKILL.md", "b\n")
	change, err = r.Pull(ctx)
	if err != nil || !change.Updated() {
		t.Fatalf("pull after an upstream commit: %+v %v", change, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "b", "SKILL.md")); err != nil {
		t.Fatal("pull did not bring the new skill")
	}
}

func TestPullRefusesADirtyClone(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "skills", "a", "SKILL.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	u.commit("skills/b/SKILL.md", "b\n")

	if _, err := r.Pull(ctx); !errors.Is(err, source.ErrDirty) {
		t.Fatalf("err = %v, want ErrDirty", err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "b")); err == nil {
		t.Fatal("pull ran on a dirty clone")
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sifatul-stack", "state.json")
	if _, err := source.LoadState(path); !errors.Is(err, source.ErrNotInstalled) {
		t.Fatalf("missing state: err = %v", err)
	}
	want := source.State{Source: "/x/stack", Managed: true, Harnesses: []string{"claude", "codex"}, Agents: []string{"senior", "junior"}}
	if err := source.SaveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := source.LoadState(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("state = %+v %v, want %+v", got, err, want)
	}
}

func TestStateFromBeforeHarnessChoiceLoadsWithNoSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"source": "/x/stack", "managed": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := source.LoadState(path)
	if err != nil || got.Harnesses != nil || got.Agents != nil {
		t.Fatalf("state = %+v %v; an old state should mean every harness and no recorded agents", got, err)
	}
}

func TestSaveStateNeverExposesAPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := source.State{Source: "/x/stack", Managed: true, Harnesses: []string{"claude", "codex", "opencode"}}
	if err := source.SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			if err := source.SaveState(path, st); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	defer func() { <-done }()
	for {
		select {
		case <-done:
			return
		default:
		}
		if _, err := source.LoadState(path); err != nil {
			t.Errorf("a reader saw a half-written state: %v", err)
			return
		}
	}
}

func TestEnsureLeavesNothingBehindWhenTheCloneFails(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	r.Git = func(ctx context.Context, args ...string) ([]byte, error) {
		if args[0] == "clone" {
			if err := os.MkdirAll(filepath.Join(args[len(args)-1], ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			return nil, errors.New("killed mid-fetch")
		}
		return source.ExecGit(ctx, args...)
	}

	if _, err := r.Ensure(context.Background()); err == nil {
		t.Fatal("ensure hid a failed clone")
	}

	if _, err := os.Lstat(r.Dir); !os.IsNotExist(err) {
		t.Errorf("a failed clone left %s behind", r.Dir)
	}
	if left, _ := os.ReadDir(filepath.Dir(r.Dir)); len(left) != 0 {
		t.Errorf("a failed clone left %v next to the clone", left)
	}
}

func TestEnsureRefusesACloneWithNothingCheckedOut(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	git(t, "init", "--quiet", r.Dir)

	_, err := r.Ensure(context.Background())

	if err == nil || !strings.Contains(err.Error(), "delete "+r.Dir) {
		t.Fatalf("err = %v, want one saying to delete %s", err, r.Dir)
	}
}

func TestExecGitLetsGitCleanUpWhenCanceled(t *testing.T) {
	bin := t.TempDir()
	started, cleaned := filepath.Join(bin, "started"), filepath.Join(bin, "cleaned")
	script := "#!/bin/sh\ntrap 'touch \"" + cleaned + "\"; exit 130' INT\ntouch \"" + started + "\"\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if _, err := os.Stat(started); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if _, err := source.ExecGit(ctx, "pull"); err == nil {
		t.Fatal("canceled git reported success")
	}
	if _, err := os.Stat(cleaned); err != nil {
		t.Fatalf("git got no chance to clean up: %v", err)
	}
}

func TestPullRefusesADivergedCloneEvenWithPullRebaseSet(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "local.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", r.Dir, "add", "-A")
	git(t, "-C", r.Dir, "commit", "--quiet", "-m", "local")
	before := git(t, "-C", r.Dir, "rev-parse", "HEAD")
	u.commit("skills/b/SKILL.md", "b\n")
	for i, kv := range [][2]string{{"pull.rebase", "true"}, {"user.name", "test"}, {"user.email", "test@example.com"}} {
		t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", i), kv[0])
		t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", i), kv[1])
	}
	t.Setenv("GIT_CONFIG_COUNT", "3")

	if _, err := r.Pull(ctx); err == nil {
		t.Fatal("pull accepted a diverged clone")
	}
	if after := git(t, "-C", r.Dir, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved from %s to %s", before, after)
	}
}

func TestEnsureChecksOutTheChosenBranch(t *testing.T) {
	u := newUpstream(t)
	git(t, "-C", u.dir, "checkout", "--quiet", "-b", "next")
	u.commit("skills/n/SKILL.md", "n\n")
	git(t, "-C", u.dir, "checkout", "--quiet", "main")
	r := repo(t, u)
	r.Branch = "next"

	if _, err := r.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "n", "SKILL.md")); err != nil {
		t.Fatal("the clone is not on the chosen branch")
	}
}

func tracks(t *testing.T, r source.Repo) bool {
	t.Helper()
	ok, err := r.Tracks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestTracksComparesTheOriginAndBranch(t *testing.T) {
	u := newUpstream(t)
	git(t, "-C", u.dir, "branch", "next")
	ctx := context.Background()
	r := repo(t, u)
	if _, err := r.Tracks(ctx); err == nil {
		t.Error("a missing clone should be an error, not a verdict")
	}
	if _, err := r.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	if !tracks(t, r) {
		t.Error("a fresh clone of the default branch should track it")
	}
	main := r
	main.Branch = "main"
	if !tracks(t, main) {
		t.Error("naming the default branch should still track")
	}
	next := r
	next.Branch = "next"
	if tracks(t, next) {
		t.Error("a clone of main does not track next")
	}
	moved := r
	moved.URL = u.dir + "-moved"
	if tracks(t, moved) {
		t.Error("a clone of another URL does not track")
	}
}

func TestTracksHonorsTheStoredURLUnderAnInsteadOfRule(t *testing.T) {
	u := newUpstream(t)
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[url \"file://"+u.dir+"\"]\n\tinsteadOf = https://example.test/up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	r := source.Repo{Dir: filepath.Join(t.TempDir(), "extras", "mp"), URL: "https://example.test/up", Git: source.ExecGit}
	if _, err := r.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !tracks(t, r) {
		t.Error("a fresh clone through an insteadOf rule should track its manifest URL")
	}
}

func TestTracksReportsAMissingOriginHeadAsAnError(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", r.Dir, "remote", "set-head", "origin", "--delete")

	if _, err := r.Tracks(ctx); err == nil {
		t.Error("an unknown default branch should be an error, not a mismatch")
	}
}

func TestSyncClonesThenPulls(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()

	change, err := r.Sync(ctx)
	if err != nil || !change.Cloned {
		t.Fatalf("first sync: %+v %v", change, err)
	}
	u.commit("skills/b/SKILL.md", "b\n")
	change, err = r.Sync(ctx)
	if err != nil || change.Cloned || !change.Updated() {
		t.Fatalf("second sync: %+v %v", change, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "b", "SKILL.md")); err != nil {
		t.Fatal("sync did not pull the new skill")
	}
}

func TestSyncReclonesWhenTheBranchChanged(t *testing.T) {
	u := newUpstream(t)
	git(t, "-C", u.dir, "checkout", "--quiet", "-b", "next")
	u.commit("skills/n/SKILL.md", "n\n")
	git(t, "-C", u.dir, "checkout", "--quiet", "main")
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	r.Branch = "next"

	change, err := r.Sync(ctx)
	if err != nil || !change.Cloned {
		t.Fatalf("sync after a branch change: %+v %v", change, err)
	}
	if !tracks(t, r) {
		t.Error("the re-clone is not on next")
	}
	if err := filepath.WalkDir(r.Dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && path != r.Dir && d.Name() == ".git" && filepath.Dir(path) != r.Dir {
			t.Errorf("the old clone was left at %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(r.Dir)); len(entries) != 1 {
		t.Errorf("leftovers next to the clone: %v", entries)
	}
}

func TestSyncRefusesToRecloneOverLocalWork(t *testing.T) {
	for name, work := range map[string]func(dir string){
		"uncommitted": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "mine.md"), []byte("mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"committed": func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "mine.md"), []byte("mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, "-C", dir, "add", "-A")
			git(t, "-C", dir, "commit", "--quiet", "-m", "mine")
		},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream(t)
			r := repo(t, u)
			ctx := context.Background()
			if _, err := r.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			git(t, "-C", r.Dir, "checkout", "--quiet", "-b", "mine")
			work(r.Dir)

			if _, err := r.Sync(ctx); !errors.Is(err, source.ErrDirty) {
				t.Fatalf("err = %v, want ErrDirty", err)
			}
			if _, err := os.Stat(filepath.Join(r.Dir, "mine.md")); err != nil {
				t.Error("sync deleted local work")
			}
		})
	}
}

func TestSyncKeepsTheOldCloneWhenTheRecloneFails(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	r.Branch = "gone"

	if _, err := r.Sync(ctx); err == nil {
		t.Fatal("sync to a missing branch reported success")
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "skills", "a", "SKILL.md")); err != nil {
		t.Error("a failed re-clone removed the old clone")
	}
}

func TestSyncStopsOnAGitErrorInsteadOfRecloning(t *testing.T) {
	u := newUpstream(t)
	r := repo(t, u)
	ctx := context.Background()
	if _, err := r.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", r.Dir, "remote", "set-head", "origin", "--delete")
	marker := filepath.Join(r.Dir, ".git", "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Sync(ctx); err == nil {
		t.Fatal("sync hid a git error")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("sync re-cloned after a git error")
	}
}
