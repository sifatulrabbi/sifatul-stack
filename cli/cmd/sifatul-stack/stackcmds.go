package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/sifatul-stack/cli/render"
	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

var (
	errStack     = errors.New("stack")
	errConflicts = errors.New("resolve the conflicts above and rerun")
)

type stackEnv struct {
	layout  stack.Layout
	dataDir string
	repoURL string
	git     source.Git
	now     func() time.Time
	animate bool
	home    string
}

func (e stackEnv) tilde(s string) string {
	if e.home == "" {
		return s
	}
	return strings.ReplaceAll(s, e.home+string(filepath.Separator), "~"+string(filepath.Separator))
}

func (e stackEnv) print(w io.Writer, s string) error { return ui.Print(w, e.tilde(s)) }

func (e stackEnv) progress(w io.Writer) ui.Progress { return ui.Progress{Out: w, Animate: e.animate} }

func (e stackEnv) note(w io.Writer, glyph, msg string) {
	e.print(w, glyph+" stack"+ui.Muted.Render(" · ")+msg+"\n")
}

func (e stackEnv) step(cmd *cobra.Command, do func(ctx context.Context) (string, error)) error {
	res := e.progress(cmd.OutOrStdout()).Run(cmd.Context(), true, ui.Task{Label: "stack", Do: func(ctx context.Context) (string, error) {
		detail, err := do(ctx)
		return e.tilde(detail), err
	}})
	return res[0].Err
}

func (e stackEnv) statePath() string { return filepath.Join(e.dataDir, "state.json") }

func (e stackEnv) repo() source.Repo {
	return source.Repo{Dir: filepath.Join(e.dataDir, "stack"), URL: e.repoURL, Git: e.git}
}

func (e stackEnv) sync(w io.Writer, src string, mode stack.Mode) error {
	r, err := stack.Syncer{Source: src, Layout: e.layout, Now: e.now}.Run(mode)
	view := render.StackSummary
	if mode == stack.Status {
		view = render.StackStatus
	}
	if werr := e.print(w, view(r.Rows)); werr != nil {
		return fmt.Errorf("%w: %w", errOutput, werr)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", errStack, err)
	}
	if r.Conflicts() > 0 {
		return errConflicts
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
				e.note(w, ui.Warn.Render("•"), "switching from "+prev.Source)
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
	if from != "" {
		dir, err := checkout(from)
		if err != nil {
			return source.State{}, err
		}
		e.note(cmd.OutOrStdout(), ui.OK.Render("✓"), "using your checkout at "+dir)
		return source.State{Source: dir}, nil
	}
	repo := e.repo()
	err := e.step(cmd, func(ctx context.Context) (string, error) {
		cloned, err := repo.Ensure(ctx)
		switch {
		case err != nil:
			return "", err
		case cloned:
			return "cloned " + repo.URL + " into " + repo.Dir, nil
		default:
			return "using the clone at " + repo.Dir, nil
		}
	})
	if err != nil {
		return source.State{}, err
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
				err := e.step(cmd, func(ctx context.Context) (string, error) {
					change, err := source.Repo{Dir: st.Source, Git: e.git}.Pull(ctx)
					switch {
					case err != nil:
						return "", err
					case change.Updated():
						return "updated " + change.From + " → " + change.To, nil
					default:
						return "already up to date at " + change.To, nil
					}
				})
				if err != nil {
					return fmt.Errorf("%w: %w", errStack, err)
				}
			} else {
				e.note(w, ui.Warn.Render("•"), st.Source+" is your checkout; pull it yourself")
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
				e.note(w, ui.Warn.Render("•"), "not set up; run `sifatul-stack setup`")
				return nil
			}
			if err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			kind := "your checkout"
			if st.Managed {
				kind = "managed clone"
			}
			e.print(w, ui.Accent.Render("Stack")+"  "+kind+ui.Muted.Render(" · "+st.Source)+"\n\n")
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
				e.note(w, ui.OK.Render("✓"), "removed; the clone at "+st.Source+" stays for the next setup")
			}
			return nil
		},
	}
}
