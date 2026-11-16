package render

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

func PrettyJSON(w io.Writer, reports []usage.Report, now time.Time) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		FetchedAt time.Time      `json:"fetched_at"`
		Reports   []usage.Report `json:"reports"`
	}{now, reports})
}

type compactWindow struct {
	Name      string `json:"name"`
	Used      int    `json:"used"`
	Left      int    `json:"left"`
	ResetsInS *int64 `json:"resets_in_s,omitempty"`
	ResetsAt  string `json:"resets_at,omitempty"`
}

type compactReport struct {
	Provider string          `json:"provider"`
	Plan     string          `json:"plan,omitempty"`
	Windows  []compactWindow `json:"windows,omitempty"`
	Stale    bool            `json:"stale,omitempty"`
	Source   string          `json:"source,omitempty"`
	AgeS     *int64          `json:"age_s,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func JSON(w io.Writer, reports []usage.Report, now time.Time) error {
	out := struct {
		FetchedAt string          `json:"fetched_at"`
		Reports   []compactReport `json:"reports"`
	}{FetchedAt: utcSecond(now)}
	for _, r := range reports {
		out.Reports = append(out.Reports, compact(r, now))
	}
	return json.NewEncoder(w).Encode(out)
}

func compact(r usage.Report, now time.Time) compactReport {
	c := compactReport{Provider: r.Provider, Plan: r.Plan, Error: r.Error}
	if r.Stale {
		c.Stale = true
		c.Source = r.Source
		if r.AsOf != nil {
			c.AgeS = seconds(now.Sub(*r.AsOf))
		}
	}
	for _, win := range r.Windows {
		if win.Spend {
			continue
		}
		used := int(math.Round(win.UsedPercent))
		cw := compactWindow{Name: win.Name, Used: used, Left: max(0, 100-used)}
		if win.ResetsAt != nil {
			cw.ResetsInS = seconds(win.ResetsAt.Sub(now))
			cw.ResetsAt = utcSecond(*win.ResetsAt)
		}
		c.Windows = append(c.Windows, cw)
	}
	return c
}

func seconds(d time.Duration) *int64 {
	s := max(0, int64(d.Round(time.Second)/time.Second))
	return &s
}

func utcSecond(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

type TextView struct {
	Width int
	Loc   *time.Location
	Color bool
}

const defaultBarWidth = 50

func (v TextView) Text(w io.Writer, reports []usage.Report, now time.Time) error {
	width := v.Width
	if width <= 0 {
		width = defaultBarWidth
	}
	var b strings.Builder
	for i, r := range reports {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(header(r, now) + "\n")
		if r.Error != "" {
			b.WriteString("  " + ui.Bad.Render("✗ error: "+r.Error) + "\n")
			continue
		}
		if len(r.Windows) == 0 {
			b.WriteString("  " + ui.Muted.Render("no active limits reported") + "\n")
		}
		for _, win := range r.Windows {
			b.WriteString("\n  " + ui.Label.Render(label(win.Name)) + "\n")
			b.WriteString("  " + ui.Bar(win.UsedPercent, width, v.Color) + "  " + ui.Figure.Render(fmt.Sprintf("%.0f%% used", win.UsedPercent)) + "\n")
			if note := v.windowNote(win, now); note != "" {
				b.WriteString("  " + ui.Muted.Render(note) + "\n")
			}
		}
		b.WriteString(costBlock(r))
		var extras [][]string
		for _, k := range slices.Sorted(maps.Keys(r.Extras)) {
			extras = append(extras, []string{"  " + ui.Muted.Render(strings.ReplaceAll(k, "_", " ")), r.Extras[k]})
		}
		if len(extras) > 0 {
			b.WriteString("\n" + ui.Columns(extras, "  "))
		}
	}
	return ui.Print(w, b.String())
}

func header(r usage.Report, now time.Time) string {
	h := ui.Accent.Render(capitalize(r.Provider))
	if r.Plan != "" {
		h += "  " + r.Plan
	}
	if r.Stale && r.AsOf != nil {
		h += "  " + ui.Warn.Render("stale · "+r.Source+", as of "+humanize(now.Sub(*r.AsOf))+" ago")
	}
	return h
}

var windowLabels = map[string]string{
	"session":       "Current session",
	"five_hour":     "Current session",
	"weekly":        "Current week",
	"seven_day":     "Current week",
	"weekly_all":    "Current week (all models)",
	"weekly_scoped": "Current week",
	"extra_usage":   "Extra usage",
}

func label(name string) string {
	base, scope, scoped := strings.Cut(name, ":")
	l, ok := windowLabels[base]
	if !ok {
		l = capitalize(strings.ReplaceAll(base, "_", " "))
	}
	if scoped {
		l += " (" + scope + ")"
	}
	return l
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (v TextView) windowNote(win usage.Window, now time.Time) string {
	switch {
	case win.Spend && win.Budget != nil:
		return money(win.Budget.Spent, win.Budget.Currency) + " of " + money(win.Budget.Limit, win.Budget.Currency) + " spent this month"
	case win.Spend:
		return "of the monthly spending limit"
	case win.ResetsAt != nil:
		return v.resetLine(*win.ResetsAt, now)
	default:
		return ""
	}
}

func (v TextView) resetLine(at, now time.Time) string {
	if !at.After(now) {
		return "Reset due"
	}
	loc := v.Loc
	if loc == nil {
		loc = time.Local
	}
	t, today := at.In(loc).Round(time.Minute), now.In(loc)
	clock := t.Format("3:04pm")
	if t.Minute() == 0 {
		clock = t.Format("3pm")
	}
	s := "Resets " + clock
	if t.YearDay() != today.YearDay() || t.Year() != today.Year() {
		s = "Resets " + t.Format("Jan 2") + " at " + clock
	}
	if zone := loc.String(); zone != "Local" {
		s += " (" + zone + ")"
	}
	return s
}

var currencySymbols = map[string]string{"USD": "$", "EUR": "€", "GBP": "£"}

func money(amount float64, currency string) string {
	if sym, ok := currencySymbols[currency]; ok {
		return fmt.Sprintf("%s%.2f", sym, amount)
	}
	return fmt.Sprintf("%s %.2f", currency, amount)
}

func humanize(d time.Duration) string {
	d = d.Round(time.Minute)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func costBlock(r usage.Report) string {
	if r.CostError != "" {
		return "\n  " + ui.Bold.Render("Week cost") + "  " + ui.Warn.Render("unavailable ("+r.CostError+")") + "\n"
	}
	if r.Cost == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s  %s  %s\n", ui.Bold.Render("Week cost"), ui.Bold.Render(fmt.Sprintf("$%.2f", r.Cost.USD)),
		ui.Muted.Render("API-equivalent since "+r.Cost.From.Local().Format("Mon Jan 2 15:04")+" · this machine's logs"))
	var dollars, tokens []string
	for _, m := range r.Cost.Models {
		d := "unpriced"
		if m.Priced {
			d = fmt.Sprintf("$%.2f", m.USD)
		}
		dollars = append(dollars, d)
		tokens = append(tokens, compactCount(m.Tokens.Total()))
	}
	var rows [][]string
	for i, m := range r.Cost.Models {
		t := ui.Muted.Render(padLeft(tokens[i], tokens) + " tokens")
		if m.PricedAs != "" {
			t += ui.Muted.Render(" (priced as " + m.PricedAs + ")")
		}
		d := padLeft(dollars[i], dollars)
		if !m.Priced {
			d = ui.Warn.Render(d)
		}
		rows = append(rows, []string{"    " + m.Model, d, t})
	}
	b.WriteString(ui.Columns(rows, "  "))
	return b.String()
}

func padLeft(s string, column []string) string {
	width := 0
	for _, c := range column {
		width = max(width, len(c))
	}
	return strings.Repeat(" ", width-len(s)) + s
}

func compactCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprint(n)
	}
}
