package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/cost"
	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/usage"
)

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(d); return &t }

func reports() []usage.Report {
	return []usage.Report{
		{Provider: "claude", Plan: "max", Windows: []usage.Window{
			{Name: "five_hour", UsedPercent: 37, ResetsAt: at(4*time.Hour + 5*time.Minute)},
			{Name: "seven_day", UsedPercent: 12.5, ResetsAt: at(3*24*time.Hour + 2*time.Hour)},
		}},
		{Provider: "codex", Error: "not logged in"},
	}
}

func TestTextShowsUsageRemainingResetAndErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, reports(), now); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"claude  max", "five_hour", "37% used", "63% left", "resets in 4h05m", "resets in 3d2h", "codex", "error: not logged in"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrettyJSONKeepsFullReport(t *testing.T) {
	var buf bytes.Buffer
	if err := PrettyJSON(&buf, reports(), now); err != nil {
		t.Fatal(err)
	}
	var got struct {
		FetchedAt time.Time      `json:"fetched_at"`
		Reports   []usage.Report `json:"reports"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.FetchedAt.Equal(now) || got.Reports[0].Windows[1].UsedPercent != 12.5 || got.Reports[1].Error != "not logged in" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestBarClampsOutOfRange(t *testing.T) {
	if got := bar(150); got != "["+strings.Repeat("#", 20)+"]" {
		t.Errorf("bar(150) = %s", got)
	}
	if got := bar(-5); got != "["+strings.Repeat(".", 20)+"]" {
		t.Errorf("bar(-5) = %s", got)
	}
}

func TestResetInThePastSaysDue(t *testing.T) {
	if got := resets(at(-time.Minute), now); got != "reset due" {
		t.Errorf("resets = %q", got)
	}
}

func TestTextFlagsStaleCachedReportAndExtras(t *testing.T) {
	asOf := now.Add(-2 * time.Hour)
	var buf bytes.Buffer
	err := Text(&buf, []usage.Report{{
		Provider: "codex", Plan: "plus", Source: "rollout-cache", AsOf: &asOf, Stale: true,
		Windows: []usage.Window{{Name: "weekly", UsedPercent: 30}},
		Extras:  map[string]string{"reset_credits_available": "3", "credits": "0.00"},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"codex  plus  (rollout-cache, as of 2h00m ago)", "credits: 0.00", "reset_credits_available: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "credits:") > strings.Index(out, "reset_credits_available") {
		t.Errorf("extras not sorted:\n%s", out)
	}
}

func TestTextFreshReportHasNoAgeSuffix(t *testing.T) {
	asOf := now
	var buf bytes.Buffer
	Text(&buf, []usage.Report{{Provider: "codex", Source: "codex app-server", AsOf: &asOf}}, now)
	if strings.Contains(buf.String(), "as of") || !strings.Contains(buf.String(), "no active limits reported") {
		t.Errorf("output:\n%s", buf.String())
	}
}

func TestCompactJSONIsOneLineWithPrecomputedFields(t *testing.T) {
	reset := time.Date(2026, 11, 16, 14, 20, 0, 954085000, time.UTC)
	var buf bytes.Buffer
	err := JSON(&buf, []usage.Report{
		{Provider: "claude", Plan: "max", Windows: []usage.Window{
			{Name: "session", UsedPercent: 2.4, ResetsAt: &reset, WindowMinutes: 300},
			{Name: "weekly", UsedPercent: 40.949999999999996},
			{Name: "extra_usage", UsedPercent: 12, Spend: true},
		}},
		{Provider: "codex", Error: "not logged in"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"fetched_at":"2026-11-16T10:00:00Z","reports":[` +
		`{"provider":"claude","plan":"max","windows":[{"name":"session","used":2,"left":98,"resets_in_s":15601,"resets_at":"2026-11-16T14:20:00Z"},{"name":"weekly","used":41,"left":59}]},` +
		`{"provider":"codex","error":"not logged in"}]}` + "\n"
	if buf.String() != want {
		t.Fatalf("got  %s\nwant %s", buf.String(), want)
	}
}

func TestCompactJSONShowsSourceAndAgeOnlyWhenStale(t *testing.T) {
	asOf := now.Add(-90 * time.Second)
	fresh := usage.Report{Provider: "codex", Source: "codex app-server", AsOf: &now}
	stale := usage.Report{Provider: "codex", Source: "rollout-cache", AsOf: &asOf, Stale: true}

	if got := compact(fresh, now); got.Stale || got.Source != "" || got.AgeS != nil {
		t.Errorf("fresh = %+v", got)
	}
	got := compact(stale, now)
	if !got.Stale || got.Source != "rollout-cache" || got.AgeS == nil || *got.AgeS != 90 {
		t.Errorf("stale = %+v", got)
	}
}

func TestCompactJSONClampsPastResetAndOverLimit(t *testing.T) {
	got := compact(usage.Report{Windows: []usage.Window{{Name: "w", UsedPercent: 104, ResetsAt: at(-time.Minute)}}}, now)
	w := got.Windows[0]
	if w.Used != 104 || w.Left != 0 || *w.ResetsInS != 0 {
		t.Errorf("window = %+v", w)
	}
}

func TestExtrasAreHiddenFromAgentsButShownToHumans(t *testing.T) {
	r := usage.Report{Provider: "codex", Extras: map[string]string{"reset_credits_available": "3"}}

	var agent bytes.Buffer
	JSON(&agent, []usage.Report{r}, now)
	if strings.Contains(agent.String(), "extras") || strings.Contains(agent.String(), "reset_credits_available") {
		t.Errorf("agent JSON leaked extras: %s", agent.String())
	}
	var human bytes.Buffer
	Text(&human, []usage.Report{r}, now)
	if !strings.Contains(human.String(), "reset_credits_available: 3") {
		t.Errorf("text output lost extras:\n%s", human.String())
	}
}

func TestSpendWindowsAreHiddenFromAgentsButShownToHumans(t *testing.T) {
	r := usage.Report{Provider: "claude", Windows: []usage.Window{{Name: "extra_usage", UsedPercent: 41, Spend: true}}}

	if got := compact(r, now); len(got.Windows) != 0 {
		t.Errorf("compact windows = %+v", got.Windows)
	}
	var buf bytes.Buffer
	Text(&buf, []usage.Report{r}, now)
	if !strings.Contains(buf.String(), "extra_usage") {
		t.Errorf("text output lost spend window:\n%s", buf.String())
	}
}

func weekCost() *cost.Summary {
	return &cost.Summary{From: now.Add(-3 * 24 * time.Hour), To: now, USD: 123.456, Models: []cost.ModelCost{
		{Model: "claude-opus-5-5", USD: 120, Priced: true, Tokens: cost.Tokens{CacheRead: 2_500_000_000}},
		{Model: "claude-haiku-5-5", USD: 3.456, Priced: true, Tokens: cost.Tokens{Output: 1500}},
		{Model: "aliased-model", USD: 0, Priced: true, PricedAs: "gpt-6.1-terra", Tokens: cost.Tokens{Output: 7}},
		{Model: "codex-auto-review", Tokens: cost.Tokens{Input: 42}},
	}}
}

func TestTextShowsWeekCostPerModelAndUnpriced(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, []usage.Report{{Provider: "claude", Cost: weekCost()}}, now)
	out := buf.String()
	for _, want := range []string{"week cost: $123.46 API-equivalent since", "claude-opus-5-5", "$120.00", "2.5B tokens", "1.5k tokens", "codex-auto-review", "unpriced", "42 tokens", "7 tokens (priced as gpt-6.1-terra)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTextShowsWhyWeekCostIsUnavailable(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, []usage.Report{{Provider: "claude", CostError: "litellm prices not installed"}}, now)
	if !strings.Contains(buf.String(), "week cost: unavailable (litellm prices not installed)") {
		t.Errorf("output:\n%s", buf.String())
	}
}

func TestWeekCostNeverReachesAgentJSON(t *testing.T) {
	var buf bytes.Buffer
	JSON(&buf, []usage.Report{{Provider: "claude", Cost: weekCost(), CostError: "x"}}, now)
	if strings.Contains(buf.String(), "cost") || strings.Contains(buf.String(), "usd") {
		t.Errorf("agent JSON leaked cost: %s", buf.String())
	}
}
