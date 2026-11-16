package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
)

var (
	errStack     = errors.New("stack")
	errConflicts = errors.New("conflicts left untouched; resolve them and rerun")
)

type stackEnv struct {
	layout  stack.Layout
	dataDir string
	repoURL string
	git     source.Git
	now     func() time.Time
}

func (e stackEnv) statePath() string { return filepath.Join(e.dataDir, "state.json") }

func (e stackEnv) repo() source.Repo {
	return source.Repo{Dir: filepath.Join(e.dataDir, "stack"), URL: e.repoURL, Git: e.git}
}

func (e stackEnv) sync(w io.Writer, src string, mode stack.Mode) error {
	r, err := stack.Syncer{Source: src, Layout: e.layout, Now: e.now}.Run(mode)
	for _, row := range r.Rows {
		fmt.Fprintf(w, "%-12s %-14s %-9s %s\n", row.Kind, row.Name, row.Harness, row.State)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", errStack, err)
	}
	if n := r.Conflicts(); n > 0 {
		return fmt.Errorf("%d %w", n, errConflicts)
	}
	return nil
}

func (e stackEnv) loadState() (source.State, error) {
	st, err := source.LoadState(e.statePath())
	if err != nil {
		return st, fmt.Errorf("%w: %w", errStack, err)
	}
	return st, nil
}

func checkout(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "agents", "models.tsv")); err != nil {
		return "", fmt.Errorf("%s is not a sifatul-stack checkout (no agents/models.tsv)", abs)
	}
	return abs, nil
}

func newSetupCmd(e stackEnv) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install the stack: clone it, link skills and global instructions, render the sub-agents",
		Long: "Install the stack into Claude Code, Codex, and OpenCode.\n\n" +
			"Clones the stack into the data dir (or uses --from), links skills and global instructions, and renders the named sub-agents. " +
			"An existing global instructions file is backed up first; `sifatul-stack remove` restores it. " +
			"Anything at a target path that the stack doesn't own is reported as a conflict and left alone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			want, err := e.resolveSource(cmd, from)
			if err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			if prev, err := source.LoadState(e.statePath()); err == nil && prev.Source != want.Source {
				fmt.Fprintf(w, "stack: switching from %s\n", prev.Source)
				if err := e.sync(w, prev.Source, stack.Unlink); err != nil {
					return err
				}
			}
			if err := source.SaveState(e.statePath(), want); err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			return e.sync(w, want.Source, stack.Link)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "use this checkout of the stack instead of a managed clone (edits show up live)")
	return cmd
}

func (e stackEnv) resolveSource(cmd *cobra.Command, from string) (source.State, error) {
	w := cmd.OutOrStdout()
	if from != "" {
		dir, err := checkout(from)
		if err != nil {
			return source.State{}, err
		}
		fmt.Fprintf(w, "stack: using your checkout at %s\n", dir)
		return source.State{Source: dir}, nil
	}
	repo := e.repo()
	cloned, err := repo.Ensure(cmd.Context())
	if err != nil {
		return source.State{}, err
	}
	if cloned {
		fmt.Fprintf(w, "stack: cloned %s into %s\n", repo.URL, repo.Dir)
	} else {
		fmt.Fprintf(w, "stack: using the clone at %s\n", repo.Dir)
	}
	dir, err := checkout(repo.Dir)
	if err != nil {
		return source.State{}, err
	}
	return source.State{Source: dir, Managed: true}, nil
}

func newUpdateCmd(e stackEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Pull the latest stack and re-sync skills, instructions, and sub-agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			st, err := e.loadState()
			if err != nil {
				return err
			}
			if st.Managed {
				change, err := source.Repo{Dir: st.Source, Git: e.git}.Pull(cmd.Context())
				if err != nil {
					return fmt.Errorf("%w: %w", errStack, err)
				}
				if change.Updated() {
					fmt.Fprintf(w, "stack: updated %s..%s\n", change.From, change.To)
				} else {
					fmt.Fprintf(w, "stack: already up to date at %s\n", change.To)
				}
			} else {
				fmt.Fprintf(w, "stack: %s is your checkout; pull it yourself\n", st.Source)
			}
			return e.sync(w, st.Source, stack.Link)
		},
	}
}

func newStatusCmd(e stackEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is linked, rendered, stale, or in conflict (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			st, err := source.LoadState(e.statePath())
			if errors.Is(err, source.ErrNotSetUp) {
				fmt.Fprintln(w, "stack: not set up; run `sifatul-stack setup`")
				return nil
			}
			if err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			kind := "your checkout"
			if st.Managed {
				kind = "managed clone"
			}
			fmt.Fprintf(w, "stack: %s (%s)\n", st.Source, kind)
			return e.sync(w, st.Source, stack.Status)
		},
	}
}

func newRemoveCmd(e stackEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove everything setup created and restore backed-up global instructions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			st, err := e.loadState()
			if err != nil {
				return err
			}
			if err := e.sync(w, st.Source, stack.Unlink); err != nil {
				return err
			}
			if err := os.Remove(e.statePath()); err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			if st.Managed {
				fmt.Fprintf(w, "stack: removed; the clone at %s stays for the next setup\n", st.Source)
			}
			return nil
		},
	}
}
