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
	choose  func(ctx context.Context, p ui.Pick) ([]string, error)
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

func (e stackEnv) step(cmd *cobra.Command, label string, do func(ctx context.Context) (string, error)) error {
	res := e.progress(cmd.OutOrStdout()).Run(cmd.Context(), true, ui.Task{Label: label, Do: func(ctx context.Context) (string, error) {
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

func (e stackEnv) extrasDir() string { return filepath.Join(e.dataDir, "extras") }

func (e stackEnv) extraRepo(x stack.Extra) source.Repo {
	return source.Repo{Dir: x.Dir(e.extrasDir()), URL: x.Repo, Branch: x.Branch, Git: e.git}
}

func pickExtras(manifest []stack.Extra, names []string) []stack.Extra {
	return slices.DeleteFunc(slices.Clone(manifest), func(x stack.Extra) bool { return !slices.Contains(names, x.Name) })
}

func extraNames(extras []stack.Extra) []string {
	out := make([]string, len(extras))
	for i, x := range extras {
		out[i] = x.Name
	}
	return out
}

func (e stackEnv) sync(ctx context.Context, w io.Writer, st source.State, mode stack.Mode, dropped ...string) error {
	if err := ui.Interrupted(ctx); err != nil {
		return err
	}
	var extras []stack.Extra
	if mode == stack.Link || mode == stack.Status {
		manifest, err := stack.LoadExtras(st.Source)
		if err != nil {
			return failed(err)
		}
		extras = pickExtras(manifest, st.Extras)
	}
	r, err := stack.Syncer{Source: st.Source, Layout: e.layout, Selected: st.Harnesses, Dropped: dropped, Owned: st.Agents, ExtrasDir: e.extrasDir(), Extras: extras}.Run(mode)
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
	var harnesses, extras []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Pull the latest sifatul-stack and install it into the chosen harnesses; clean up the ones you drop",
		Long: "Install sifatul-stack into Claude Code, Codex, and OpenCode, or the ones you choose.\n\n" +
			"Every run pulls the latest sifatul-stack into the data dir (cloning it the first time), links skills and global instructions, and renders the named sub-agents. " +
			"In a terminal, install asks which harnesses to install into; --harness skips the question, and without either it keeps the last choice (all on a first install). " +
			"A harness you drop from the list is cleaned up: its links and sub-agent files are removed, and nothing is restored. A harness you never chose is never touched. " +
			"Whatever already sits at a target path (a skill link, a global instructions file, a sub-agent file) is replaced without a backup. " +
			"--from links your own checkout instead of the managed clone; install never pulls it.\n\n" +
			"Extras are other skill repos listed in the stack's extras.json. In a terminal, install asks which extras to add after the harnesses; --extras skips the question, and without either it keeps the last choice (none on a first install). " +
			"Each chosen extra is cloned into the data dir, or pulled when already there (re-cloned when its repo or branch changed, unless the clone holds local work), and its listed skills are linked next to the stack's own. " +
			"An extra you drop has its links and its clone removed.",
		Example: "  sifatul-stack install\n  sifatul-stack install --harness claude,codex\n  sifatul-stack install --extras mattpocock\n  sifatul-stack install --extras none\n  sifatul-stack install --from ~/projects/sifatul-stack",
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
			manifest, err := stack.LoadExtras(want.Source)
			if err != nil {
				return failed(err)
			}
			picked, err := e.chooseExtras(cmd, extras, manifest, prev, prevErr == nil)
			if err != nil {
				return err
			}
			if err := e.fetchExtras(cmd, picked); err != nil {
				return failed(err)
			}
			want.Extras = extraNames(picked)
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
			err = e.sync(cmd.Context(), w, want, stack.Link, dropped...)
			if err == nil || errors.Is(err, errFailed) {
				if rerr := e.removeClonesExcept(picked); rerr != nil {
					return failed(rerr)
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "use this checkout of sifatul-stack instead of the managed clone; install links it and never pulls it")
	cmd.Flags().StringSliceVar(&harnesses, "harness", nil, "install only into these harnesses: "+strings.Join(stack.Harnesses, ", ")+" (repeat or comma-separate)")
	cmd.Flags().StringSliceVar(&extras, "extras", nil, "install exactly these extras from extras.json, or none (repeat or comma-separate)")
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
	chosen, err := e.choose(cmd.Context(), ui.Pick{Title: "Install sifatul-stack into which harnesses?", Choices: choices})
	if errors.Is(err, ui.ErrCanceled) {
		return nil, failed(errors.New("install canceled; nothing changed"))
	}
	if err != nil {
		return nil, failed(err)
	}
	return stack.ParseHarnesses(chosen)
}

func (e stackEnv) chooseExtras(cmd *cobra.Command, flag []string, manifest []stack.Extra, prev source.State, hasPrev bool) ([]stack.Extra, error) {
	if cmd.Flags().Changed("extras") {
		return parseExtras(flag, manifest)
	}
	var last []string
	if hasPrev {
		last = prev.Extras
	}
	if e.choose == nil || len(manifest) == 0 {
		return pickExtras(manifest, last), nil
	}
	choices := make([]ui.Choice, len(manifest))
	for i, x := range manifest {
		detail := x.Repo
		if x.Branch != "" {
			detail += "@" + x.Branch
		}
		hint := fmt.Sprintf("%d skills", len(x.Skills))
		if len(x.Skills) == 1 {
			hint = "1 skill"
		}
		was := slices.Contains(last, x.Name)
		choices[i] = ui.Choice{Key: x.Name, Label: x.Name, Detail: e.tilde(detail), Hint: hint, On: was, Was: was}
	}
	chosen, err := e.choose(cmd.Context(), ui.Pick{Title: "Install which extras?", Choices: choices, AllowEmpty: true})
	if errors.Is(err, ui.ErrCanceled) {
		return nil, failed(errors.New("install canceled; nothing linked"))
	}
	if err != nil {
		return nil, failed(err)
	}
	return pickExtras(manifest, chosen), nil
}

func parseExtras(names []string, manifest []stack.Extra) ([]stack.Extra, error) {
	var want []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		switch {
		case n == "" || n == "none":
			continue
		case !slices.Contains(extraNames(manifest), n):
			known := "extras.json lists none"
			if len(manifest) > 0 {
				known = "choose from " + strings.Join(extraNames(manifest), ", ") + ", or none"
			}
			return nil, fmt.Errorf("unknown extra %q; %s", n, known)
		}
		want = append(want, n)
	}
	return pickExtras(manifest, want), nil
}

func (e stackEnv) fetchExtras(cmd *cobra.Command, extras []stack.Extra) error {
	for _, x := range extras {
		if err := e.step(cmd, x.Name, syncStep(e.extraRepo(x))); err != nil {
			return err
		}
	}
	return nil
}

func (e stackEnv) removeClonesExcept(keep []stack.Extra) error {
	entries, err := os.ReadDir(e.extrasDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	kept := make([]string, len(keep))
	for i, x := range keep {
		kept[i] = x.Dir(e.extrasDir())
	}
	for _, d := range entries {
		dir := filepath.Join(e.extrasDir(), d.Name())
		if !slices.Contains(kept, dir) {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncStep(repo source.Repo) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		change, err := repo.Sync(ctx)
		switch {
		case err != nil:
			return "", err
		case change.Cloned:
			return "cloned " + repo.URL + " into " + repo.Dir, nil
		case change.Updated():
			return "updated " + change.From + " → " + change.To, nil
		default:
			return "already up to date at " + change.To, nil
		}
	}
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
	if err := e.step(cmd, brand, syncStep(repo)); err != nil {
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
			extras := "none"
			if len(st.Extras) > 0 {
				extras = strings.Join(st.Extras, ", ")
			}
			e.print(w, ui.Columns([][]string{
				{ui.Accent.Render(brand), kind + ui.Muted.Render(" · "+st.Source)},
				{ui.Accent.Render("Harnesses"), labels(chosen)},
				{ui.Accent.Render("Extras"), extras},
			}, "  ")+"\n")
			return e.sync(cmd.Context(), w, st, stack.Status)
		},
	}
}
