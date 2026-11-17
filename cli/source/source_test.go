package source_test

import (
	"context"
	"errors"
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
	if _, err := source.LoadState(path); !errors.Is(err, source.ErrNotSetUp) {
		t.Fatalf("missing state: err = %v", err)
	}
	want := source.State{Source: "/x/stack", Managed: true, Harnesses: []string{"claude", "codex"}}
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
	if err != nil || got.Harnesses != nil {
		t.Fatalf("state = %+v %v; an old state should mean every harness", got, err)
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

func TestPullRefusesADivergedCloneAndKeepsLocalCommits(t *testing.T) {
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

	if _, err := r.Pull(ctx); err == nil {
		t.Fatal("pull accepted a diverged clone")
	}
	if after := git(t, "-C", r.Dir, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved from %s to %s", before, after)
	}
}
