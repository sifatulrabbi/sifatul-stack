package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/sifatul-stack/cli/render"
	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

const brand = "sifatul-stack"

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
	choose  func(ctx context.Context, choices []ui.Choice) ([]string, error)
	found   func(h stack.Harness) bool
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
	e.print(w, glyph+" "+brand+ui.Muted.Render(" · ")+msg+"\n")
}

func (e stackEnv) step(cmd *cobra.Command, do func(ctx context.Context) (string, error)) error {
	res := e.progress(cmd.OutOrStdout()).Run(cmd.Context(), true, ui.Task{Label: brand, Do: func(ctx context.Context) (string, error) {
		detail, err := do(ctx)
		return e.tilde(detail), err
	}})
	return res[0].Err
}

func (e stackEnv) statePath() string { return filepath.Join(e.dataDir, "state.json") }

func (e stackEnv) repo() source.Repo {
	return source.Repo{Dir: filepath.Join(e.dataDir, "stack"), URL: e.repoURL, Git: e.git}
}

func (e stackEnv) sync(w io.Writer, st source.State, mode stack.Mode) error {
	r, err := stack.Syncer{Source: st.Source, Layout: e.layout, Now: e.now, Selected: st.Harnesses}.Run(mode)
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
	var harnesses []string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install sifatul-stack: clone it, link skills and global instructions, render the sub-agents",
		Long: "Install sifatul-stack into Claude Code, Codex, and OpenCode, or the ones you choose.\n\n" +
			"Clones sifatul-stack into the data dir (or uses --from), links skills and global instructions, and renders the named sub-agents. " +
			"In a terminal, setup asks which harnesses to install into; --harness skips the question, and without either it keeps the last choice (all on a first setup). " +
			"A harness you drop is cleaned up the same way `sifatul-stack remove` would. " +
			"An existing global instructions file is backed up first; `sifatul-stack remove` restores it. " +
			"Anything at a target path that sifatul-stack doesn't own is reported as a conflict and left alone.",
		Example: "  sifatul-stack setup\n  sifatul-stack setup --harness claude,codex\n  sifatul-stack setup --from ~/projects/sifatul-stack",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			prev, prevErr := source.LoadState(e.statePath())
			chosen, err := e.chooseHarnesses(cmd, harnesses, prev, prevErr == nil)
			if err != nil {
				return err
			}
			e.note(w, ui.OK.Render("✓"), "installing into "+labels(chosen))
			want, err := e.resolveSource(cmd, from)
			if err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			want.Harnesses = chosen
			if prevErr == nil && prev.Source != want.Source {
				e.note(w, ui.Warn.Render("•"), "switching from "+prev.Source)
				if err := e.sync(w, prev, stack.Detach); err != nil {
					return err
				}
			}
			if err := source.SaveState(e.statePath(), want); err != nil {
				return fmt.Errorf("%w: %w", errStack, err)
			}
			return e.sync(w, want, stack.Link)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "use this checkout of sifatul-stack instead of a managed clone (edits show up live)")
	cmd.Flags().StringSliceVar(&harnesses, "harness", nil, "install only into these harnesses: "+strings.Join(stack.Harnesses, ", ")+" (repeat or comma-separate)")
	return cmd
}

func labels(harnesses []string) string {
	out := make([]string, len(harnesses))
	for i, h := range harnesses {
		out[i] = stack.HarnessLabel(h)
	}
	return strings.Join(out, ", ")
}

func (e stackEnv) chooseHarnesses(cmd *cobra.Command, flag []string, prev source.State, hasPrev bool) ([]string, error) {
	last := stack.Chosen(nil)
	if hasPrev {
		last = stack.Chosen(prev.Harnesses)
	}
	switch {
	case cmd.Flags().Changed("harness"):
		return stack.ParseHarnesses(flag)
	case e.choose == nil:
		return last, nil
	}
	harnesses := stack.All()
	choices := make([]ui.Choice, len(harnesses))
	for i, h := range harnesses {
		found := e.found != nil && e.found(h)
		hint := "not found"
		if found {
			hint = "found"
		}
		on := found
		if hasPrev {
			on = slices.Contains(last, h.Key)
		}
		choices[i] = ui.Choice{
			Key: h.Key, Label: h.Label, Detail: e.tilde(h.Dir(e.layout)), Hint: hint,
			On: on, Was: hasPrev && slices.Contains(last, h.Key),
		}
	}
	chosen, err := e.choose(cmd.Context(), choices)
	if errors.Is(err, ui.ErrCanceled) {
		return nil, fmt.Errorf("%w: setup canceled; nothing changed", errStack)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errStack, err)
	}
	return stack.ParseHarnesses(chosen)
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
		Short: "Pull the latest sifatul-stack and re-sync skills, instructions, and sub-agents",
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
			return e.sync(w, st, stack.Link)
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
			chosen := stack.Chosen(st.Harnesses)
			e.print(w, ui.Columns([][]string{
				{ui.Accent.Render(brand), kind + ui.Muted.Render(" · "+st.Source)},
				{ui.Accent.Render("Harnesses"), labels(chosen)},
			}, "  ")+"\n")
			return e.sync(w, st, stack.Status)
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
			if err := e.sync(w, st, stack.Unlink); err != nil {
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
