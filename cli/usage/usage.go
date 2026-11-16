package usage

import (
	"context"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

type Window struct {
	Name          string     `json:"name"`
	UsedPercent   float64    `json:"used_percent"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	WindowMinutes int        `json:"window_minutes,omitempty"`
	Spend         bool       `json:"spend,omitempty"`
}

func (w Window) RemainingPercent() float64 {
	r := 100 - w.UsedPercent
	if r < 0 {
		return 0
	}
	return r
}

type Report struct {
	Provider  string            `json:"provider"`
	Plan      string            `json:"plan,omitempty"`
	Source    string            `json:"source,omitempty"`
	AsOf      *time.Time        `json:"as_of,omitempty"`
	Stale     bool              `json:"stale,omitempty"`
	Windows   []Window          `json:"windows"`
	Extras    map[string]string `json:"extras,omitempty"`
	Cost      *cost.Summary     `json:"cost,omitempty"`
	CostError string            `json:"cost_error,omitempty"`
	Error     string            `json:"error,omitempty"`
}

const weekMinutes = 7 * 24 * 60

func (r Report) WeeklyWindow() (Window, bool) {
	for _, w := range r.Windows {
		if w.WindowMinutes == weekMinutes && !w.Spend && w.ResetsAt != nil {
			return w, true
		}
	}
	return Window{}, false
}

func (r *Report) SetExtra(key, value string) {
	if r.Extras == nil {
		r.Extras = map[string]string{}
	}
	r.Extras[key] = value
}

type Provider interface {
	Name() string
	Usage(ctx context.Context) (Report, error)
}

func Collect(ctx context.Context, providers []Provider) []Report {
	reports := make([]Report, len(providers))
	done := make(chan struct{}, len(providers))
	for i, p := range providers {
		go func() {
			defer func() { done <- struct{}{} }()
			r, err := p.Usage(ctx)
			if err != nil {
				r = Report{Provider: p.Name(), Error: err.Error()}
			}
			if r.Provider == "" {
				r.Provider = p.Name()
			}
			reports[i] = r
		}()
	}
	for range providers {
		<-done
	}
	return reports
}
