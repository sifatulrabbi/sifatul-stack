package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const DefaultURL = "https://github.com/sifatulrabbi/sifatul-stack"

var (
	ErrDirty    = errors.New("the sifatul-stack clone has local changes; commit, stash, or discard them first")
	ErrNotSetUp = errors.New("sifatul-stack is not set up; run `sifatul-stack setup` first")
)

const gitGrace = 5 * time.Second

type Git func(ctx context.Context, args ...string) ([]byte, error)

func ExecGit(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = gitGrace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

type Repo struct {
	Dir string
	URL string
	Git Git
}

func (r Repo) Ensure(ctx context.Context) (cloned bool, err error) {
	if _, err := os.Stat(r.Dir); errors.Is(err, os.ErrNotExist) {
		if err := r.clone(ctx); err != nil {
			return false, err
		}
		return true, nil
	}
	if _, err := r.git(ctx, "rev-parse", "--is-inside-work-tree"); err != nil {
		return false, fmt.Errorf("%s exists but is not a git clone: %w", r.Dir, err)
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return false, fmt.Errorf("%s has nothing checked out, likely from an interrupted clone; delete %s and rerun setup", r.Dir, r.Dir)
	}
	return false, nil
}

func (r Repo) clone(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(r.Dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(r.Dir), "."+filepath.Base(r.Dir)+".clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := r.Git(ctx, "clone", "--quiet", r.URL, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, r.Dir)
}

type Change struct {
	From string
	To   string
}

func (c Change) Updated() bool { return c.From != c.To }

func (r Repo) Pull(ctx context.Context) (Change, error) {
	status, err := r.git(ctx, "status", "--porcelain")
	if err != nil {
		return Change{}, err
	}
	if len(strings.TrimSpace(string(status))) > 0 {
		return Change{}, fmt.Errorf("%s: %w", r.Dir, ErrDirty)
	}
	from, err := r.head(ctx)
	if err != nil {
		return Change{}, err
	}
	if _, err := r.git(ctx, "pull", "--ff-only", "--quiet"); err != nil {
		return Change{}, err
	}
	to, err := r.head(ctx)
	return Change{From: from, To: to}, err
}

func (r Repo) head(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(string(out)), err
}

func (r Repo) git(ctx context.Context, args ...string) ([]byte, error) {
	return r.Git(ctx, append([]string{"-C", r.Dir}, args...)...)
}

type State struct {
	Source    string   `json:"source"`
	Managed   bool     `json:"managed"`
	Harnesses []string `json:"harnesses,omitempty"`
}

func LoadState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotSetUp
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("%s: %w", path, err)
	}
	if s.Source == "" {
		return State{}, fmt.Errorf("%s: no source recorded", path)
	}
	return s, nil
}

func SaveState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(raw, '\n'))
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
