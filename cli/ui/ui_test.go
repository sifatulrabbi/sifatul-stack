package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBarFillsAndColorsByUsage(t *testing.T) {
	for _, tt := range []struct {
		used  float64
		fill  string
		level string
	}{
		{0, "░░░░░░░░░░", "ok"},
		{49, "█████░░░░░", "ok"},
		{50, "█████░░░░░", "warn"},
		{80, "████████░░", "bad"},
		{130, "██████████", "bad"},
		{-5, "░░░░░░░░░░", "ok"},
	} {
		if got := Plain(Bar(tt.used, 10, false)); got != tt.fill {
			t.Errorf("Bar(%v) = %q, want %q", tt.used, got, tt.fill)
		}
		if got := levelName(tt.used); got != tt.level {
			t.Errorf("level(%v) = %s, want %s", tt.used, got, tt.level)
		}
	}
}

func TestSolidTrackKeepsTheWidth(t *testing.T) {
	if got := Plain(Bar(30, 10, true)); got != "██████████" {
		t.Errorf("solid bar = %q", got)
	}
}

func TestColumnsAlignByVisibleWidth(t *testing.T) {
	got := Plain(Columns([][]string{
		{OK.Render("✓"), "a", "first"},
		{"✗", "longer", "second"},
		{"•", "c", Muted.Render("")},
	}, "  "))
	want := "✓  a       first\n✗  longer  second\n•  c\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func tasks(fail error) []Task {
	return []Task{
		{Label: "claude", Do: func(context.Context) (string, error) { return "team", nil }},
		{Label: "codex", Do: func(context.Context) (string, error) { return "", fail }},
	}
}

func TestRunWithoutAnimationKeepsFinalLinesInTaskOrder(t *testing.T) {
	var buf bytes.Buffer
	res := Progress{Out: &buf}.Run(context.Background(), true, tasks(errors.New("not logged in"))...)
	if res[0].Err != nil || res[0].Detail != "team" || res[1].Err == nil {
		t.Fatalf("results = %+v", res)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "✓ claude · team") || !strings.HasPrefix(lines[1], "✗ codex · not logged in") {
		t.Errorf("output:\n%s", buf.String())
	}
}

func TestRunWithoutAnimationOrKeepPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	Progress{Out: &buf}.Run(context.Background(), false, tasks(nil)...)
	if buf.Len() != 0 {
		t.Errorf("transient run printed %q", buf.String())
	}
}

func TestModelShowsProgressThenClearsForTheFinalLines(t *testing.T) {
	m := newModel(tasks(nil), true)
	if v := Plain(m.View().Content); !strings.Contains(v, "claude …") || !strings.Contains(v, "codex …") {
		t.Fatalf("initial view = %q", v)
	}
	next, _ := m.Update(doneMsg{i: 0, res: Result{Detail: "team"}})
	m = next.(model)
	if v := Plain(m.View().Content); !strings.Contains(v, "✓ claude · team") || !strings.Contains(v, "codex …") {
		t.Errorf("view after claude finished = %q", v)
	}
	next, _ = m.Update(doneMsg{i: 1})
	m = next.(model)
	if !m.finished() || m.View().Content != "" {
		t.Errorf("finished=%v view=%q", m.finished(), Plain(m.View().Content))
	}
	if _, cmd := m.Update(allDoneMsg{}); cmd == nil {
		t.Error("model did not quit once every task was done")
	}
}

func TestTransientModelClearsWhenDone(t *testing.T) {
	m := newModel(tasks(nil), false)
	next, _ := m.Update(doneMsg{i: 0})
	next, _ = next.(model).Update(doneMsg{i: 1})
	if v := next.(model).View().Content; v != "" {
		t.Errorf("transient view = %q", v)
	}
}

func TestProgressLinesStripTerminalEscapes(t *testing.T) {
	osc := "\x1b]52;c;ZWNobyBwd25lZA==\x07"
	for _, out := range []string{
		line("label"+osc, Result{Err: errors.New("git: fatal" + osc)}),
		line("label", Result{Detail: "abc\r" + osc}),
		newModel([]Task{{Label: "clone" + osc}}, false).View().Content,
	} {
		for _, bad := range []string{"\x1b]", "\x07", "\r"} {
			if strings.Contains(out, bad) {
				t.Errorf("line carries %q: %q", bad, out)
			}
		}
	}
}

func TestSafeKeepsTextAndLayoutWhitespace(t *testing.T) {
	if got := Safe("a\tb\nc\x1b[31md\x1b[0m\u0085e\x00f"); got != "a\tb\ncdef" {
		t.Errorf("Safe = %q", got)
	}
}

func TestAnimatedRunReturnsWhenEveryTaskIsDone(t *testing.T) {
	done := make(chan []Result, 1)
	var buf bytes.Buffer
	go func() { done <- Progress{Out: &buf, Animate: true}.Run(context.Background(), true, tasks(nil)...) }()
	select {
	case res := <-done:
		if res[0].Detail != "team" || !strings.Contains(Plain(buf.String()), "✓ claude · team") {
			t.Errorf("results %+v output %q", res, Plain(buf.String()))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("animated Run never returned")
	}
}

func TestSlowTaskShowsItsDuration(t *testing.T) {
	if got := Plain(line("x", Result{Took: 99 * time.Millisecond})); got != "✓ x" {
		t.Errorf("fast = %q", got)
	}
	if got := Plain(line("x", Result{Took: 1500 * time.Millisecond})); got != "✓ x  1.5s" {
		t.Errorf("slow = %q", got)
	}
}

func TestInterruptStopsRunWithoutPrintingResults(t *testing.T) {
	for _, animate := range []bool{false, true} {
		ctx, cancel := context.WithCancelCause(context.Background())
		waits := Task{Label: "codex", Do: func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}}
		time.AfterFunc(50*time.Millisecond, func() { cancel(ErrInterrupted) })
		var buf bytes.Buffer
		done := make(chan struct{})
		go func() {
			defer close(done)
			Progress{Out: &buf, Animate: animate}.Run(ctx, true, waits)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("animate=%v: Run did not return after an interrupt", animate)
		}
		if !errors.Is(Interrupted(ctx), ErrInterrupted) || strings.Contains(Plain(buf.String()), "✗") {
			t.Errorf("animate=%v: interrupted=%v output %q", animate, Interrupted(ctx), Plain(buf.String()))
		}
	}
}

func TestATimeoutIsNotAnInterrupt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if err := Interrupted(ctx); err != nil {
		t.Errorf("Interrupted = %v", err)
	}
}
