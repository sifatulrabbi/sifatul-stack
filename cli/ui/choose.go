package ui

import (
	"context"
	"errors"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
)

var ErrCanceled = errors.New("canceled")

type Choice struct {
	Key    string
	Label  string
	Detail string
	Hint   string
	On     bool
	Was    bool
}

type Pick struct {
	Title      string
	Choices    []Choice
	AllowEmpty bool
}

func Choose(ctx context.Context, in io.Reader, out io.Writer, p Pick) ([]string, error) {
	m := newChooser(p.Title, p.Choices)
	m.allowEmpty = p.AllowEmpty
	final, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx)).Run()
	if err != nil {
		return nil, err
	}
	m = final.(chooser)
	if m.canceled {
		return nil, ErrCanceled
	}
	return m.selected(), nil
}

type chooser struct {
	title      string
	choices    []Choice
	cursor     int
	allowEmpty bool
	empty      bool
	done       bool
	canceled   bool
}

func newChooser(title string, choices []Choice) chooser {
	return chooser{title: title, choices: append([]Choice(nil), choices...)}
}

func (m chooser) selected() []string {
	var keys []string
	for _, c := range m.choices {
		if c.On {
			keys = append(keys, c.Key)
		}
	}
	return keys
}

func (m chooser) Init() tea.Cmd { return nil }

func (m chooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	m.choices = append([]Choice(nil), m.choices...)
	m.empty = false
	switch key.String() {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.choices)-1, m.cursor+1)
	case "space", "x":
		m.choices[m.cursor].On = !m.choices[m.cursor].On
	case "a":
		all := len(m.selected()) < len(m.choices)
		for i := range m.choices {
			m.choices[i].On = all
		}
	case "enter":
		if len(m.selected()) == 0 && !m.allowEmpty {
			m.empty = true
			return m, nil
		}
		m.done = true
		return m, tea.Quit
	case "esc", "q", "ctrl+c":
		m.canceled, m.done = true, true
		return m, tea.Quit
	}
	return m, nil
}

func (m chooser) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	rows := make([][]string, len(m.choices))
	for i, c := range m.choices {
		pointer, label := "  ", c.Label
		if i == m.cursor {
			pointer, label = Accent.Render("❯ "), Bold.Render(c.Label)
		}
		box := Muted.Render("○")
		if c.On {
			box = OK.Render("◉")
		}
		hint := Muted.Render(c.Hint)
		if c.Was && !c.On {
			hint = Warn.Render("will be removed")
		}
		rows[i] = []string{pointer + box + " " + label, Muted.Render(c.Detail), hint}
	}
	var b strings.Builder
	b.WriteString(Label.Render(m.title) + "\n\n")
	for _, line := range strings.Split(strings.TrimSuffix(Columns(rows, "  "), "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("\n")
	if m.empty {
		b.WriteString("  " + Warn.Render("select at least one") + "\n")
	}
	b.WriteString(Muted.Render("  ↑/↓ move · space toggle · a all · enter confirm · esc cancel"))
	return tea.NewView(b.String())
}
