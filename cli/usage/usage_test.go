package usage

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvider struct {
	name   string
	report Report
	err    error
}

func (f fakeProvider) Name() string                          { return f.name }
func (f fakeProvider) Usage(context.Context) (Report, error) { return f.report, f.err }

func TestCollectKeepsOrderAndTurnsErrorsIntoReports(t *testing.T) {
	reports := Collect(context.Background(), []Provider{
		fakeProvider{name: "a", report: Report{Windows: []Window{{Name: "5h", UsedPercent: 10}}}},
		fakeProvider{name: "b", err: errors.New("boom")},
	})

	if reports[0].Provider != "a" || len(reports[0].Windows) != 1 {
		t.Fatalf("first report = %+v", reports[0])
	}
	if reports[1].Provider != "b" || reports[1].Error != "boom" {
		t.Fatalf("second report = %+v", reports[1])
	}
}

func TestRemainingPercentNeverNegative(t *testing.T) {
	if got := (Window{UsedPercent: 130}).RemainingPercent(); got != 0 {
		t.Fatalf("remaining = %v, want 0", got)
	}
	if got := (Window{UsedPercent: 25}).RemainingPercent(); got != 75 {
		t.Fatalf("remaining = %v, want 75", got)
	}
}

func TestWeeklyMeansASevenDayLimitWithAReset(t *testing.T) {
	reset := time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		w    Window
		want bool
	}{
		{Window{Name: "weekly_all", WindowMinutes: 10080, ResetsAt: &reset}, true},
		{Window{Name: "session", WindowMinutes: 300, ResetsAt: &reset}, false},
		{Window{Name: "weekly_nil_reset", WindowMinutes: 10080}, false},
		{Window{Name: "extra_usage", WindowMinutes: 10080, ResetsAt: &reset, Spend: true}, false},
	} {
		if got := tt.w.Weekly(); got != tt.want {
			t.Errorf("%s: Weekly() = %v, want %v", tt.w.Name, got, tt.want)
		}
	}
}
