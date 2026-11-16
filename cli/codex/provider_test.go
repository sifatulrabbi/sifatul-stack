package codex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubSource struct {
	reading Reading
	err     error
}

func (s stubSource) Read(context.Context) (Reading, error) { return s.reading, s.err }

func weekly(used float64) *Window {
	reset := fixedNow.Add(48 * time.Hour)
	return &Window{UsedPercent: used, WindowMinutes: 10080, ResetsAt: &reset}
}

func TestProviderUsesLiveReading(t *testing.T) {
	n := 3
	p := Provider{
		Live:     stubSource{reading: Reading{Source: "codex app-server", AsOf: fixedNow, ResetCreditsAvailable: &n, Snapshots: []Snapshot{{LimitID: "codex", PlanType: "plus", Primary: weekly(30), CreditsBalance: "0E-10"}}}},
		Fallback: stubSource{err: errors.New("must not be used")},
	}
	r, err := p.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Stale || r.Plan != "plus" || r.Source != "codex app-server" || r.Extras["reset_credits_available"] != "3" || r.Extras["credits"] != "0.00" {
		t.Fatalf("report = %+v", r)
	}
	if len(r.Windows) != 1 || r.Windows[0].Name != "weekly" || r.Windows[0].UsedPercent != 30 {
		t.Fatalf("windows = %+v", r.Windows)
	}
}

func TestProviderFallsBackAndRecordsLiveError(t *testing.T) {
	p := Provider{
		Live:     stubSource{err: errors.New("app-server down")},
		Fallback: stubSource{reading: Reading{Source: "rollout-cache", AsOf: fixedNow, Snapshots: []Snapshot{{LimitID: "codex", Primary: weekly(10)}}}},
	}
	r, err := p.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Stale || r.Source != "rollout-cache" || r.AsOf == nil || r.Extras["live_error"] != "app-server down" {
		t.Fatalf("report = %+v", r)
	}
}

func TestProviderReportsBothErrors(t *testing.T) {
	p := Provider{Live: stubSource{err: errors.New("live bad")}, Fallback: stubSource{err: ErrNoSnapshot}}
	_, err := p.Usage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "live bad") || !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderEmptyReadingIsNoSnapshot(t *testing.T) {
	_, err := Provider{Live: stubSource{}}.Usage(context.Background())
	if !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("err = %v", err)
	}
}

func TestToReportNamesWindowsByDurationNotSlot(t *testing.T) {
	session := &Window{UsedPercent: 5, WindowMinutes: 300}
	r := ToReport([]Snapshot{
		{LimitID: "codex_other", Primary: &Window{UsedPercent: 1, WindowMinutes: 60}},
		{LimitID: "codex", PlanType: "pro", Primary: weekly(40), Secondary: session, CreditsUnlimited: true},
	})
	var names []string
	for _, w := range r.Windows {
		names = append(names, w.Name)
	}
	if got := strings.Join(names, ","); got != "session,weekly,codex_other:60m" {
		t.Fatalf("windows = %s", got)
	}
	if r.Plan != "pro" || r.Extras["credits"] != "unlimited" {
		t.Fatalf("report = %+v", r)
	}
}
