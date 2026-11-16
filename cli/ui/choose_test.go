package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(m chooser, keys ...string) (chooser, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		var key tea.KeyPressMsg
		switch k {
		case "up":
			key = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			key = tea.KeyPressMsg{Code: tea.KeyDown}
		case "space":
			key = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "enter":
			key = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			key = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			key = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		next, c := m.Update(key)
		m, cmd = next.(chooser), c
	}
	return m, cmd
}

func harnessChoices() []Choice {
	return []Choice{
		{Key: "claude", Label: "Claude Code", Detail: "~/.claude", Hint: "found", On: true, Was: true},
		{Key: "codex", Label: "Codex", Detail: "~/.codex", Hint: "found", On: true, Was: true},
		{Key: "opencode", Label: "OpenCode", Detail: "~/.config/opencode", Hint: "not found"},
	}
}

func TestChooserTogglesAndConfirms(t *testing.T) {
	m, cmd := press(newChooser("Install into", harnessChoices()), "down", "space", "down", "space", "enter")

	if cmd == nil || m.canceled {
		t.Fatal("enter should quit with a choice")
	}
	if got := m.selected(); !slices.Equal(got, []string{"claude", "opencode"}) {
		t.Errorf("selected %v, want [claude opencode]", got)
	}
}

func TestChooserCursorStaysInBounds(t *testing.T) {
	m, _ := press(newChooser("t", harnessChoices()), "up", "k", "down", "down", "down", "j")
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
}

func TestChooserAllTogglesEveryChoice(t *testing.T) {
	m, _ := press(newChooser("t", harnessChoices()), "a")
	if got := m.selected(); len(got) != 3 {
		t.Errorf("a should select all, got %v", got)
	}
	m, _ = press(m, "a")
	if got := m.selected(); len(got) != 0 {
		t.Errorf("a again should clear all, got %v", got)
	}
}

func TestChooserRefusesAnEmptyChoice(t *testing.T) {
	m, cmd := press(newChooser("t", harnessChoices()), "a", "a", "enter")
	if cmd != nil {
		t.Fatal("enter with nothing selected should not quit")
	}
	if !strings.Contains(Plain(m.View().Content), "select at least one") {
		t.Errorf("view lacks the hint:\n%s", Plain(m.View().Content))
	}
}

func TestChooserCancels(t *testing.T) {
	for _, k := range []string{"esc", "q", "ctrl+c"} {
		m := newChooser("t", harnessChoices())
		var cmd tea.Cmd
		if k == "ctrl+c" {
			next, c := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			m, cmd = next.(chooser), c
		} else {
			m, cmd = press(m, k)
		}
		if cmd == nil || !m.canceled {
			t.Errorf("%s should cancel", k)
		}
	}
}

func TestChooserViewMarksRemovalsAndClearsWhenDone(t *testing.T) {
	m, _ := press(newChooser("Install into", harnessChoices()), "down", "space")
	view := Plain(m.View().Content)
	for _, want := range []string{"Install into", "  ◉ Claude Code", "❯ ○ Codex", "will be removed", "~/.config/opencode", "not found", "space toggle"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Count(view, "will be removed") != 1 {
		t.Errorf("only codex was deselected:\n%s", view)
	}
	m, _ = press(m, "enter")
	if m.View().Content != "" {
		t.Error("view should clear once confirmed")
	}
}
