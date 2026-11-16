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

func TestWeeklyWindowIsFirstSevenDayLimitWithAReset(t *testing.T) {
	reset := time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)
	r := Report{Windows: []Window{
		{Name: "session", WindowMinutes: 300, ResetsAt: &reset},
		{Name: "weekly_nil_reset", WindowMinutes: 10080},
		{Name: "weekly_all", WindowMinutes: 10080, ResetsAt: &reset},
		{Name: "weekly_scoped:Fable", WindowMinutes: 10080, ResetsAt: &reset},
	}}
	w, ok := r.WeeklyWindow()
	if !ok || w.Name != "weekly_all" {
		t.Fatalf("weekly = %+v, %v", w, ok)
	}
	if _, ok := (Report{Windows: []Window{{Name: "session", WindowMinutes: 300, ResetsAt: &reset}}}).WeeklyWindow(); ok {
		t.Fatal("session-only report has no weekly window")
	}
}
