package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/sifatul-stack/cli/render"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

var (
	errProviderFailed = errors.New("at least one provider failed")
	errOutput         = errors.New("write output")
	errPricesUpdate   = errors.New("update prices")
)

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errProviderFailed), errors.Is(err, errOutput), errors.Is(err, errPricesUpdate):
		return 1
	default:
		return 2
	}
}

type app struct {
	providers    []usage.Provider
	now          func() time.Time
	weekly       weeklyCost
	updatePrices func(ctx context.Context) (int, error)
	pricesPath   string
}

func newRootCmd(a app) *cobra.Command {
	root := &cobra.Command{
		Use:           "sifatul-stack",
		Short:         "Set up and update Sifatul's agent stack, and report coding-agent usage",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newUsageCmd(a))
	return root
}

func newUsageCmd(a app) *cobra.Command {
	var asJSON, pretty bool
	var timeout time.Duration
	known := names(a.providers)

	cmd := &cobra.Command{
		Use:   "usage [provider ...]",
		Short: "Show remaining subscription quota for coding-agent harnesses",
		Long: fmt.Sprintf("Show remaining subscription quota for coding-agent harnesses.\n\nProviders: %v (default: all).\n\n"+
			"The table (and --json --pretty) also shows this week's API-equivalent cost, computed from local session logs\n"+
			"and LiteLLM prices (install them once with `sifatul-stack usage prices update`). Compact --json omits it.\n\n"+
			"Exit codes: 0 all providers reported, 1 a provider failed (its report carries \"error\"), 2 bad usage.", known),
		Example:       "  sifatul-stack usage\n  sifatul-stack usage --json\n  sifatul-stack usage codex\n  sifatul-stack usage prices update",
		Args:          cobra.OnlyValidArgs,
		ValidArgs:     known,
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(*cobra.Command, []string) error {
			if pretty && !asJSON {
				return errors.New("--pretty requires --json")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			selected := selectProviders(a.providers, args)
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			reports := usage.Collect(ctx, selected)
			humanView := !asJSON || pretty
			if humanView {
				a.weekly.attach(ctx, reports, a.now())
			}

			write := render.Text
			switch {
			case asJSON && pretty:
				write = render.PrettyJSON
			case asJSON:
				write = render.JSON
			}
			if err := write(cmd.OutOrStdout(), reports, a.now()); err != nil {
				return fmt.Errorf("%w: %w", errOutput, err)
			}
			if slices.ContainsFunc(reports, func(r usage.Report) bool { return r.Error != "" }) {
				return errProviderFailed
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print compact one-line JSON for agents (rounded percents, precomputed resets_in_s)")
	cmd.Flags().BoolVar(&pretty, "pretty", false, "with --json: print the full, indented report (for debugging)")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "overall timeout")
	cmd.AddCommand(newPricesCmd(a))
	return cmd
}

func newPricesCmd(a app) *cobra.Command {
	prices := &cobra.Command{
		Use:   "prices",
		Short: "Manage the local LiteLLM model price list used for the weekly API-equivalent cost",
	}
	prices.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Download LiteLLM's model price list and install it locally",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			n, err := a.updatePrices(cmd.Context())
			if err != nil {
				return fmt.Errorf("%w: %w", errPricesUpdate, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %d model prices to %s\n", n, a.pricesPath)
			return nil
		},
	})
	return prices
}

func names(ps []usage.Provider) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return out
}

func selectProviders(all []usage.Provider, wanted []string) []usage.Provider {
	if len(wanted) == 0 {
		return all
	}
	out := make([]usage.Provider, 0, len(wanted))
	for _, w := range wanted {
		i := slices.IndexFunc(all, func(p usage.Provider) bool { return p.Name() == w })
		out = append(out, all[i])
	}
	return out
}
