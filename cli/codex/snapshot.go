package codex

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

type Window struct {
	UsedPercent   float64
	WindowMinutes int
	ResetsAt      *time.Time
}

type Snapshot struct {
	LimitID          string
	PlanType         string
	Primary          *Window
	Secondary        *Window
	CreditsBalance   string
	CreditsUnlimited bool
}

func windowName(minutes int) string {
	switch minutes {
	case 5 * 60:
		return "session"
	case 7 * 24 * 60:
		return "weekly"
	default:
		return strconv.Itoa(minutes) + "m"
	}
}

func ToReport(snapshots []Snapshot) usage.Report {
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].LimitID == "codex" && snapshots[j].LimitID != "codex"
	})

	r := usage.Report{Provider: "codex", Windows: []usage.Window{}}
	for _, s := range snapshots {
		if r.Plan == "" {
			r.Plan = s.PlanType
		}
		var windows []Window
		for _, w := range []*Window{s.Primary, s.Secondary} {
			if w != nil {
				windows = append(windows, *w)
			}
		}
		sort.SliceStable(windows, func(i, j int) bool { return windows[i].WindowMinutes < windows[j].WindowMinutes })
		for _, w := range windows {
			name := windowName(w.WindowMinutes)
			if s.LimitID != "" && s.LimitID != "codex" {
				name = s.LimitID + ":" + name
			}
			r.Windows = append(r.Windows, usage.Window{Name: name, UsedPercent: w.UsedPercent, ResetsAt: w.ResetsAt, WindowMinutes: w.WindowMinutes})
		}
		if s.LimitID == "codex" || len(snapshots) == 1 {
			addCredits(&r, s)
		}
	}
	return r
}

func addCredits(r *usage.Report, s Snapshot) {
	switch {
	case s.CreditsUnlimited:
		r.SetExtra("credits", "unlimited")
	case s.CreditsBalance != "":
		if f, err := strconv.ParseFloat(s.CreditsBalance, 64); err == nil {
			r.SetExtra("credits", fmt.Sprintf("%.2f", f))
		} else {
			r.SetExtra("credits", s.CreditsBalance)
		}
	}
}

func unixPtr(sec *int64) *time.Time {
	if sec == nil {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
}
