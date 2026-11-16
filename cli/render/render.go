package render

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

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

func Text(w io.Writer, reports []usage.Report, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range reports {
		header := r.Provider
		if r.Plan != "" {
			header += "  " + r.Plan
		}
		if r.Stale && r.AsOf != nil {
			header += "  (" + r.Source + ", as of " + humanize(now.Sub(*r.AsOf)) + " ago)"
		}
		fmt.Fprintln(tw, header)
		if r.Error != "" {
			fmt.Fprintf(tw, "  error: %s\n", r.Error)
			continue
		}
		if len(r.Windows) == 0 {
			fmt.Fprintln(tw, "  no active limits reported")
		}
		for _, win := range r.Windows {
			fmt.Fprintf(tw, "  %s\t%s\t%3.0f%% used\t%3.0f%% left\t%s\n",
				win.Name, bar(win.UsedPercent), win.UsedPercent, win.RemainingPercent(), resets(win.ResetsAt, now))
		}
		writeCost(tw, r)
		for _, k := range slices.Sorted(maps.Keys(r.Extras)) {
			fmt.Fprintf(tw, "  %s: %s\n", k, r.Extras[k])
		}
	}
	return tw.Flush()
}

func bar(used float64) string {
	const width = 20
	filled := int(used/100*width + 0.5)
	filled = max(0, min(width, filled))
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

func resets(at *time.Time, now time.Time) string {
	if at == nil {
		return ""
	}
	d := at.Sub(now)
	if d <= 0 {
		return "reset due"
	}
	return "resets in " + humanize(d) + " (" + at.Local().Format("Mon 15:04") + ")"
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

func writeCost(w io.Writer, r usage.Report) {
	if r.CostError != "" {
		fmt.Fprintf(w, "  week cost: unavailable (%s)\n", r.CostError)
		return
	}
	if r.Cost == nil {
		return
	}
	fmt.Fprintf(w, "  week cost: $%.2f API-equivalent since %s, this machine's logs\n", r.Cost.USD, r.Cost.From.Local().Format("Mon Jan 2 15:04"))
	for _, m := range r.Cost.Models {
		if m.Priced && m.PricedAs != "" {
			fmt.Fprintf(w, "    %s\t$%.2f\t%s tokens (priced as %s)\n", m.Model, m.USD, compactCount(m.Tokens.Total()), m.PricedAs)
		} else if m.Priced {
			fmt.Fprintf(w, "    %s\t$%.2f\t%s tokens\n", m.Model, m.USD, compactCount(m.Tokens.Total()))
		} else {
			fmt.Fprintf(w, "    %s\tunpriced\t%s tokens\n", m.Model, compactCount(m.Tokens.Total()))
		}
	}
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
