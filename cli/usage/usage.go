package usage

import (
	"context"
	"sync"
	"time"
)

type Window struct {
	Name          string     `json:"name"`
	UsedPercent   float64    `json:"used_percent"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	WindowMinutes int        `json:"window_minutes,omitempty"`
	Spend         bool       `json:"spend,omitempty"`
	Budget        *Budget    `json:"budget,omitempty"`
}

type Budget struct {
	Spent    float64 `json:"spent"`
	Limit    float64 `json:"limit"`
	Currency string  `json:"currency"`
}

func (w Window) RemainingPercent() float64 {
	r := 100 - w.UsedPercent
	if r < 0 {
		return 0
	}
	return r
}

type Report struct {
	Provider string            `json:"provider"`
	Plan     string            `json:"plan,omitempty"`
	Source   string            `json:"source,omitempty"`
	AsOf     *time.Time        `json:"as_of,omitempty"`
	Stale    bool              `json:"stale,omitempty"`
	Windows  []Window          `json:"windows"`
	Extras   map[string]string `json:"extras,omitempty"`
	Error    string            `json:"error,omitempty"`
}

const weekMinutes = 7 * 24 * 60

func (w Window) Weekly() bool { return w.WindowMinutes == weekMinutes && !w.Spend && w.ResetsAt != nil }

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

func Fetch(ctx context.Context, p Provider) Report {
	r, err := p.Usage(ctx)
	if err != nil {
		r = Report{Provider: p.Name(), Error: err.Error()}
	}
	if r.Provider == "" {
		r.Provider = p.Name()
	}
	return r
}

func Collect(ctx context.Context, providers []Provider) []Report {
	reports := make([]Report, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Go(func() { reports[i] = Fetch(ctx, p) })
	}
	wg.Wait()
	return reports
}
