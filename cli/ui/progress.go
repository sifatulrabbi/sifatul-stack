package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

type Task struct {
	Label string
	Do    func(ctx context.Context) (detail string, err error)
}

type Result struct {
	Detail string
	Err    error
	Took   time.Duration
}

type Progress struct {
	Out     io.Writer
	Animate bool
}

func (p Progress) Run(ctx context.Context, keep bool, tasks ...Task) []Result {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]Result, len(tasks))
	var prog *tea.Program
	if p.Animate {
		prog = tea.NewProgram(newModel(tasks, keep), tea.WithOutput(p.Out), tea.WithInput(nil), tea.WithContext(ctx))
	}
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Go(func() {
			start := time.Now()
			detail, err := t.Do(ctx)
			results[i] = Result{Detail: detail, Err: err, Took: time.Since(start)}
			if prog != nil {
				prog.Send(doneMsg{i: i, res: results[i]})
			}
		})
	}
	if prog != nil {
		go func() {
			wg.Wait()
			prog.Send(allDoneMsg{})
		}()
		if _, err := prog.Run(); err != nil {
			cancel()
		}
	}
	wg.Wait()
	if keep {
		var b strings.Builder
		for i, t := range tasks {
			b.WriteString(line(t.Label, results[i]) + "\n")
		}
		Print(p.Out, b.String())
	}
	return results
}

func line(label string, r Result) string {
	if r.Err != nil {
		return Bad.Render("✗") + " " + label + Muted.Render(" · ") + Bad.Render(r.Err.Error())
	}
	s := OK.Render("✓") + " " + label
	if r.Detail != "" {
		s += Muted.Render(" · ") + r.Detail
	}
	if r.Took >= 100*time.Millisecond {
		s += Muted.Render(fmt.Sprintf("  %.1fs", r.Took.Seconds()))
	}
	return s
}

type doneMsg struct {
	i   int
	res Result
}

type allDoneMsg struct{}

type model struct {
	tasks   []Task
	results []*Result
	keep    bool
	spin    spinner.Model
}

func newModel(tasks []Task, keep bool) model {
	return model{
		tasks:   tasks,
		results: make([]*Result, len(tasks)),
		keep:    keep,
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(Accent)),
	}
}

func (m model) finished() bool {
	for _, r := range m.results {
		if r == nil {
			return false
		}
	}
	return true
}

func (m model) Init() tea.Cmd { return m.spin.Tick }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case doneMsg:
		m.results = append([]*Result(nil), m.results...)
		m.results[msg.i] = &msg.res
		return m, nil
	case allDoneMsg:
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.finished() {
		return tea.NewView("")
	}
	var b strings.Builder
	for i, t := range m.tasks {
		switch r := m.results[i]; {
		case r == nil:
			b.WriteString(m.spin.View() + " " + t.Label + Muted.Render(" …") + "\n")
		case m.keep:
			b.WriteString(line(t.Label, *r) + "\n")
		}
	}
	return tea.NewView(strings.TrimSuffix(b.String(), "\n"))
}
