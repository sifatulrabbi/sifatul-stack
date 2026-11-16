package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
		if got := Plain(Bar(tt.used, 10)); got != tt.fill {
			t.Errorf("Bar(%v) = %q, want %q", tt.used, got, tt.fill)
		}
		if got := levelName(tt.used); got != tt.level {
			t.Errorf("level(%v) = %s, want %s", tt.used, got, tt.level)
		}
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
