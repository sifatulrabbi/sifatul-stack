package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { t := now.Add(d); return &t }

func reports() []usage.Report {
	return []usage.Report{
		{Provider: "claude", Plan: "max", Windows: []usage.Window{
			{Name: "five_hour", UsedPercent: 37, ResetsAt: at(4*time.Hour + 5*time.Minute)},
			{Name: "seven_day", UsedPercent: 12.5, ResetsAt: at(3*24*time.Hour + 2*time.Hour), WindowMinutes: 7 * 24 * 60},
		}},
		{Provider: "codex", Error: "not logged in"},
	}
}

var dhaka = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		panic(err)
	}
	return loc
}()

func text(t *testing.T, v TextView, rs ...usage.Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := v.Text(&buf, rs, now); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestTextStacksLabelBarAndResetPerWindow(t *testing.T) {
	out := text(t, TextView{Width: 20, Loc: dhaka}, reports()...)
	want := "Claude  max\n" +
		"\n" +
		"  Current session\n" +
		"  " + strings.Repeat("█", 7) + strings.Repeat("░", 13) + "  37% used\n" +
		"  Resets 8:05pm (Asia/Dhaka)\n" +
		"\n" +
		"  Current week\n" +
		"  " + strings.Repeat("█", 3) + strings.Repeat("░", 17) + "  12% used\n" +
		"  Week Nov 12 6pm – Nov 19 6pm (Asia/Dhaka) · resets in 3d2h\n" +
		"\n" +
		"Codex\n" +
		"  ✗ error: not logged in\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestSolidTrackOnlyWhenColorsShowIt(t *testing.T) {
	r := usage.Report{Provider: "claude", Windows: []usage.Window{{Name: "session", UsedPercent: 50}}}
	if out := text(t, TextView{Width: 10, Color: true}, r); !strings.Contains(out, strings.Repeat("█", 10)+"  50% used") {
		t.Errorf("colored bar:\n%s", out)
	}
	if out := text(t, TextView{Width: 10}, r); !strings.Contains(out, "█████░░░░░  50% used") {
		t.Errorf("plain bar:\n%s", out)
	}
}

func TestResetLineRoundsToTheMinuteAndDropsZeroMinutes(t *testing.T) {
	morning := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 16, 8, 19, 59, 954_000_000, time.UTC), "Resets 2:20pm (Asia/Dhaka)"},
		{time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC), "Resets Nov 20 at 1am (Asia/Dhaka)"},
		{morning.Add(-time.Minute), "Reset due"},
	} {
		if got := (TextView{Loc: dhaka}).resetLine(tt.at, morning); got != tt.want {
			t.Errorf("resetLine(%v) = %q, want %q", tt.at, got, tt.want)
		}
	}
	if got := (TextView{Loc: time.FixedZone("Local", 0)}).resetLine(now.Add(time.Hour), now); got != "Resets 11am" {
		t.Errorf("unnamed zone: %q", got)
	}
}

func TestWeeklyWindowShowsTheWeekItCovers(t *testing.T) {
	morning := time.Date(2026, 11, 16, 7, 0, 0, 0, time.UTC)
	weekly := func(at time.Time) usage.Window {
		return usage.Window{Name: "weekly_all", ResetsAt: &at, WindowMinutes: 7 * 24 * 60}
	}
	for _, tt := range []struct {
		win  usage.Window
		want string
	}{
		{weekly(time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)), "Week Nov 13 1am – Nov 20 1am (Asia/Dhaka) · resets in 3d12h"},
		{weekly(time.Date(2026, 11, 19, 19, 29, 40, 0, time.UTC)), "Week Nov 13 1:30am – Nov 20 1:30am (Asia/Dhaka) · resets in 3d12h"},
		{weekly(morning.Add(-time.Minute)), "Reset due"},
		{usage.Window{Name: "five_hour", ResetsAt: at(time.Hour), WindowMinutes: 300}, "Resets 5pm (Asia/Dhaka)"},
	} {
		if got := (TextView{Loc: dhaka}).windowNote(tt.win, morning); got != tt.want {
			t.Errorf("windowNote(%s %v) = %q, want %q", tt.win.Name, tt.win.ResetsAt, got, tt.want)
		}
	}
}

func TestWindowLabelsMatchClaudeCode(t *testing.T) {
	for name, want := range map[string]string{
		"session":             "Current session",
		"five_hour":           "Current session",
		"weekly":              "Current week",
		"weekly_all":          "Current week (all models)",
		"weekly_scoped:Fable": "Current week (Fable)",
		"extra_usage":         "Extra usage",
		"seven_day_opus":      "Seven day opus",
	} {
		if got := label(name); got != want {
			t.Errorf("label(%q) = %q, want %q", name, got, want)
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

func TestTextFlagsStaleCachedReportAndExtras(t *testing.T) {
	asOf := now.Add(-2 * time.Hour)
	var buf bytes.Buffer
	err := TextView{}.Text(&buf, []usage.Report{{
		Provider: "codex", Plan: "plus", Source: "rollout-cache", AsOf: &asOf, Stale: true,
		Windows: []usage.Window{{Name: "weekly", UsedPercent: 30}},
		Extras:  map[string]string{"reset_credits_available": "3", "credits": "0.00"},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Codex  plus  stale · rollout-cache, as of 2h00m ago", "credits                  0.00", "reset credits available  3"} {
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
	TextView{}.Text(&buf, []usage.Report{{Provider: "codex", Source: "codex app-server", AsOf: &asOf}}, now)
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
	TextView{}.Text(&human, []usage.Report{r}, now)
	if !strings.Contains(human.String(), "reset credits available  3") {
		t.Errorf("text output lost extras:\n%s", human.String())
	}
}

func TestSpendWindowsAreHiddenFromAgentsButShownToHumans(t *testing.T) {
	r := usage.Report{Provider: "claude", Windows: []usage.Window{{Name: "extra_usage", UsedPercent: 41, Spend: true}}}

	if got := compact(r, now); len(got.Windows) != 0 {
		t.Errorf("compact windows = %+v", got.Windows)
	}
	var buf bytes.Buffer
	TextView{}.Text(&buf, []usage.Report{r}, now)
	if !strings.Contains(buf.String(), "Extra usage") {
		t.Errorf("text output lost spend window:\n%s", buf.String())
	}
}

func TestSpendWindowSaysWhatItIsAPercentOf(t *testing.T) {
	tests := []struct {
		budget *usage.Budget
		want   string
	}{
		{&usage.Budget{Spent: 8.2, Limit: 20, Currency: "EUR"}, "€8.20 of €20.00 spent this month"},
		{&usage.Budget{Spent: 1, Limit: 50, Currency: "USD"}, "$1.00 of $50.00 spent this month"},
		{&usage.Budget{Spent: 1, Limit: 50, Currency: "JPY"}, "JPY 1.00 of JPY 50.00 spent this month"},
		{nil, "of the monthly spending limit"},
	}
	for _, tt := range tests {
		r := usage.Report{Provider: "claude", Windows: []usage.Window{{Name: "extra_usage", UsedPercent: 41, Spend: true, Budget: tt.budget}}}
		var buf bytes.Buffer
		TextView{}.Text(&buf, []usage.Report{r}, now)
		if !strings.Contains(buf.String(), tt.want) {
			t.Errorf("budget %+v: output lacks %q:\n%s", tt.budget, tt.want, buf.String())
		}
	}
}

const osc52 = "\x1b]52;c;ZWNobyBwd25lZA==\x07"

func assertNoTerminalControl(t *testing.T, out string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07", "\r", "\u009b"} {
		if strings.Contains(out, bad) {
			t.Errorf("output carries %q to the terminal:\n%q", bad, out)
		}
	}
}

func TestTextViewStripsTerminalEscapesFromProviderStrings(t *testing.T) {
	for _, env := range [][2]string{{"COLORTERM", "truecolor"}, {"NO_COLOR", "1"}} {
		t.Run(env[0], func(t *testing.T) {
			t.Setenv("TTY_FORCE", "1")
			t.Setenv("TERM", "xterm-256color")
			t.Setenv(env[0], env[1])
			asOf := now.Add(-time.Hour)
			reports := []usage.Report{
				{Provider: "codex", Plan: "pro" + osc52, Stale: true, AsOf: &asOf, Source: "cache\r", Extras: map[string]string{"credits\u009b2J": "\x1b[2J\x1b[H9.99"},
					Windows: []usage.Window{
						{Name: "weekly:\x1b]8;;https://evil.example\x07click\x1b]8;;\x07", UsedPercent: 10},
						{Name: "extra_usage", Spend: true, Budget: &usage.Budget{Spent: 1, Limit: 2, Currency: "X" + osc52}},
					}},
				{Provider: "claude", Error: "boom " + osc52},
			}
			var buf bytes.Buffer
			if err := (TextView{Color: true}).Text(&buf, reports, now); err != nil {
				t.Fatal(err)
			}
			assertNoTerminalControl(t, buf.String())
			for _, want := range []string{"pro", "boom", "click", "9.99"} {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output lost %q:\n%q", want, buf.String())
				}
			}
		})
	}
}

func TestCompactJSONIsUTCEvenWhenNowIsLocal(t *testing.T) {
	reset := now.Add(time.Hour).In(dhaka)
	var buf bytes.Buffer
	if err := JSON(&buf, []usage.Report{{Provider: "claude", Windows: []usage.Window{{Name: "session", ResetsAt: &reset}}}}, now.In(dhaka)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"fetched_at":"2026-11-16T10:00:00Z"`, `"resets_at":"2026-11-16T11:00:00Z"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %s in %s", want, buf.String())
		}
	}
}

func TestStaleReportWithoutAsOfHasNoAge(t *testing.T) {
	r := usage.Report{Provider: "codex", Source: "rollout-cache", Stale: true}
	if c := compact(r, now); !c.Stale || c.AgeS != nil {
		t.Fatalf("compact = %+v", c)
	}
	var buf bytes.Buffer
	if err := (TextView{}).Text(&buf, []usage.Report{r}, now); err != nil {
		t.Fatal(err)
	}
}
