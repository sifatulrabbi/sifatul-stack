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

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/sifatul-stack/cli/render"
	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

const brand = "sifatul-stack"

var errFailed = errors.New("fix the errors above and rerun")

type stackEnv struct {
	layout  stack.Layout
	dataDir string
	repoURL string
	git     source.Git
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
	if err := ui.Interrupted(cmd.Context()); err != nil {
		return err
	}
	return res[0].Err
}

func (e stackEnv) statePath() string { return filepath.Join(e.dataDir, "state.json") }

func (e stackEnv) repo() source.Repo {
	return source.Repo{Dir: filepath.Join(e.dataDir, "stack"), URL: e.repoURL, Git: e.git}
}

func (e stackEnv) sync(ctx context.Context, w io.Writer, st source.State, mode stack.Mode, dropped ...string) error {
	if err := ui.Interrupted(ctx); err != nil {
		return err
	}
	r, err := stack.Syncer{Source: st.Source, Layout: e.layout, Selected: st.Harnesses, Dropped: dropped, Owned: st.Agents}.Run(mode)
	view := render.StackSummary
	if mode == stack.Status {
		view = render.StackStatus
	}
	if werr := e.print(w, view(r.Rows)); werr != nil {
		return fmt.Errorf("%w: %w", errOutput, werr)
	}
	if errors.Is(err, stack.ErrUnknownHarness) {
		err = fmt.Errorf("%s: %w", e.statePath(), err)
	}
	if err != nil {
		return failed(err)
	}
	if mode == stack.Link {
		st.Agents = r.Agents
		if err := source.SaveState(e.statePath(), st); err != nil {
			return failed(err)
		}
	}
	if r.Failures() > 0 {
		return errFailed
	}
	return nil
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

func newInstallCmd(e stackEnv) *cobra.Command {
	var from string
	var harnesses []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Pull the latest sifatul-stack and install it into the chosen harnesses; clean up the ones you drop",
		Long: "Install sifatul-stack into Claude Code, Codex, and OpenCode, or the ones you choose.\n\n" +
			"Every run pulls the latest sifatul-stack into the data dir (cloning it the first time), links skills and global instructions, and renders the named sub-agents. " +
			"In a terminal, install asks which harnesses to install into; --harness skips the question, and without either it keeps the last choice (all on a first install). " +
			"A harness you drop from the list is cleaned up: its links and sub-agent files are removed, and nothing is restored. A harness you never chose is never touched. " +
			"Whatever already sits at a target path (a skill link, a global instructions file, a sub-agent file) is replaced without a backup. " +
			"--from links your own checkout instead of the managed clone; install never pulls it.",
		Example: "  sifatul-stack install\n  sifatul-stack install --harness claude,codex\n  sifatul-stack install --from ~/projects/sifatul-stack",
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
				return failed(err)
			}
			want.Harnesses = chosen
			var dropped []string
			if prevErr == nil {
				want.Agents = prev.Agents
				dropped = slices.DeleteFunc(stack.Chosen(prev.Harnesses), func(h string) bool { return slices.Contains(chosen, h) })
			}
			if prevErr == nil && prev.Source != want.Source {
				e.note(w, ui.Warn.Render("•"), "switching from "+prev.Source)
				if err := e.sync(cmd.Context(), w, prev, stack.Detach); err != nil {
					return err
				}
			}
			if err := source.SaveState(e.statePath(), want); err != nil {
				return failed(err)
			}
			return e.sync(cmd.Context(), w, want, stack.Link, dropped...)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "use this checkout of sifatul-stack instead of the managed clone; install links it and never pulls it")
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
		return nil, failed(errors.New("install canceled; nothing changed"))
	}
	if err != nil {
		return nil, failed(err)
	}
	return stack.ParseHarnesses(chosen)
}

func (e stackEnv) resolveSource(cmd *cobra.Command, from string) (source.State, error) {
	if from != "" {
		dir, err := checkout(from)
		if err != nil {
			return source.State{}, err
		}
		e.note(cmd.OutOrStdout(), ui.Warn.Render("•"), "using your checkout at "+dir+"; pull it yourself")
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
		}
		change, err := repo.Pull(ctx)
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
		return source.State{}, err
	}
	dir, err := checkout(repo.Dir)
	if err != nil {
		return source.State{}, err
	}
	return source.State{Source: dir, Managed: true}, nil
}

func newStatusCmd(e stackEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is linked, rendered, stale, or failed (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			st, err := source.LoadState(e.statePath())
			if errors.Is(err, source.ErrNotInstalled) {
				e.note(w, ui.Warn.Render("•"), "not installed; run `sifatul-stack install`")
				return nil
			}
			if err != nil {
				return failed(err)
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
			return e.sync(cmd.Context(), w, st, stack.Status)
		},
	}
}
