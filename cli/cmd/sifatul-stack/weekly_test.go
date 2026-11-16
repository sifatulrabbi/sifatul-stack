package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

type fakeLog struct {
	entries []cost.Entry
	err     error
	since   *time.Time
}

func (f fakeLog) Entries(_ context.Context, since time.Time) ([]cost.Entry, error) {
	if f.since != nil {
		*f.since = since
	}
	return f.entries, f.err
}

type flatBook struct{}

func (flatBook) Lookup(string) (cost.Price, bool) { return cost.Price{Output: 1}, true }

type weeklyProvider struct{ name string }

func (p weeklyProvider) Name() string { return p.name }

func (p weeklyProvider) Usage(context.Context) (usage.Report, error) {
	reset := fixedNow().Add(48 * time.Hour)
	return usage.Report{Provider: p.name, Windows: []usage.Window{{Name: "weekly", UsedPercent: 30, WindowMinutes: 10080, ResetsAt: &reset}}}, nil
}

func prices() (cost.PriceBook, error) { return flatBook{}, nil }

func TestWeekStartsSevenDaysBeforeWeeklyReset(t *testing.T) {
	var since time.Time
	reports := []usage.Report{{Provider: "codex", Windows: []usage.Window{{Name: "weekly", WindowMinutes: 10080, ResetsAt: ptr(fixedNow().Add(48 * time.Hour))}}}}
	weeklyCost{Logs: map[string]cost.Log{"codex": fakeLog{since: &since, entries: []cost.Entry{{Time: fixedNow().Add(-time.Hour), Model: "m", Tokens: cost.Tokens{Output: 7}}}}}, Prices: prices}.
		attach(context.Background(), reports, fixedNow())

	if want := fixedNow().Add(48*time.Hour - 7*24*time.Hour); !since.Equal(want) {
		t.Fatalf("since = %s, want %s", since, want)
	}
	if reports[0].Cost == nil || reports[0].Cost.USD != 7 || !reports[0].Cost.To.Equal(fixedNow()) {
		t.Fatalf("cost = %+v", reports[0].Cost)
	}
}

func TestNoWeeklyWindowNoErrorOrMissingLogMeansNoCost(t *testing.T) {
	called := false
	reports := []usage.Report{
		{Provider: "claude", Windows: []usage.Window{{Name: "session", WindowMinutes: 300, ResetsAt: ptr(fixedNow())}}},
		{Provider: "codex", Error: "down"},
		{Provider: "gemini", Windows: []usage.Window{{Name: "weekly", WindowMinutes: 10080, ResetsAt: ptr(fixedNow())}}},
	}
	weeklyCost{Logs: map[string]cost.Log{"claude": fakeLog{}, "codex": fakeLog{}}, Prices: func() (cost.PriceBook, error) {
		called = true
		return flatBook{}, nil
	}}.attach(context.Background(), reports, fixedNow())

	for _, r := range reports {
		if r.Cost != nil || r.CostError != "" {
			t.Errorf("%s got cost %+v / %q", r.Provider, r.Cost, r.CostError)
		}
	}
	if called {
		t.Error("prices loaded although no report needed them")
	}
}

func TestPriceAndLogFailuresBecomeCostErrors(t *testing.T) {
	weekly := []usage.Window{{Name: "weekly", WindowMinutes: 10080, ResetsAt: ptr(fixedNow())}}
	reports := []usage.Report{{Provider: "codex", Windows: weekly}}
	weeklyCost{Logs: map[string]cost.Log{"codex": fakeLog{}}, Prices: func() (cost.PriceBook, error) { return nil, errors.New("not installed") }}.
		attach(context.Background(), reports, fixedNow())
	if reports[0].CostError != "not installed" {
		t.Fatalf("cost error = %q", reports[0].CostError)
	}

	reports = []usage.Report{{Provider: "codex", Windows: weekly}}
	weeklyCost{Logs: map[string]cost.Log{"codex": fakeLog{err: errors.New("unreadable")}}, Prices: prices}.
		attach(context.Background(), reports, fixedNow())
	if reports[0].CostError != "unreadable" {
		t.Fatalf("cost error = %q", reports[0].CostError)
	}
}

func costApp(logCalls *int) app {
	return app{
		providers: []usage.Provider{weeklyProvider{name: "codex"}},
		now:       fixedNow,
		weekly:    weeklyCost{Logs: map[string]cost.Log{"codex": countingLog{calls: logCalls}}, Prices: prices},
	}
}

type countingLog struct{ calls *int }

func (c countingLog) Entries(context.Context, time.Time) ([]cost.Entry, error) {
	*c.calls++
	return []cost.Entry{{Time: fixedNow().Add(-time.Hour), Model: "m", Tokens: cost.Tokens{Output: 3}}}, nil
}

func run(t *testing.T, a app, args ...string) (string, error) {
	t.Helper()
	cmd := newUsageCmd(a)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestCompactJSONSkipsCostScanButHumanViewsComputeIt(t *testing.T) {
	calls := 0
	out, err := run(t, costApp(&calls), "--json")
	if err != nil || calls != 0 || strings.Contains(out, "cost") {
		t.Fatalf("compact json: calls = %d, err = %v, out = %s", calls, err, out)
	}

	out, err = run(t, costApp(&calls))
	if err != nil || calls != 1 || !strings.Contains(out, "week cost: $3.00") {
		t.Fatalf("text: calls = %d, err = %v, out = %s", calls, err, out)
	}

	out, err = run(t, costApp(&calls), "--json", "--pretty")
	if err != nil || calls != 2 || !strings.Contains(out, `"usd": 3`) {
		t.Fatalf("pretty: calls = %d, err = %v, out = %s", calls, err, out)
	}
}

func TestPricesUpdateReportsCountAndPath(t *testing.T) {
	a := app{now: fixedNow, pricesPath: "/cache/prices.json", updatePrices: func(context.Context) (int, error) { return 42, nil }}
	out, err := run(t, a, "prices", "update")
	if err != nil || !strings.Contains(out, "installed 42 model prices to /cache/prices.json") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

func TestPricesUpdateFailureExitsOne(t *testing.T) {
	a := app{now: fixedNow, updatePrices: func(context.Context) (int, error) { return 0, errors.New("offline") }}
	_, err := run(t, a, "prices", "update")
	if exitCode(err) != 1 || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
}

func ptr(t time.Time) *time.Time { return &t }
