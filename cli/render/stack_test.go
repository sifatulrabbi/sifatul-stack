package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

func row(kind, name, harness, state, change string) stack.Row {
	return stack.Row{Kind: kind, Name: name, Harness: harness, State: state, Change: change}
}

func freshSetup() []stack.Row {
	var rows []stack.Row
	for _, sk := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		rows = append(rows, row("skill", sk, "claude", "linked", "linked"), row("skill", sk, "agents", "linked", "linked"))
	}
	rows = append(rows,
		row("instructions", "AGENTS.md", "claude", "backed up to CLAUDE.md.backup-20261116120000", "backed up"),
		row("instructions", "AGENTS.md", "claude", "linked", "linked"),
		row("instructions", "AGENTS.md", "codex", "linked", ""),
		row("agent", "senior", "claude", "current", ""),
		row("agent", "senior", "codex", "written", "re-rendered"),
		row("agent", "senior", "opencode", "off, current", ""),
		stack.Row{Kind: "skill", Name: "x", Harness: "claude", State: "conflict: /h/.claude/skills/x exists", Conflict: true},
	)
	return rows
}

func plainSummary(t *testing.T, rows []stack.Row) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ui.Print(&buf, StackSummary(rows)); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestStackSummaryGroupsChangesByKind(t *testing.T) {
	out := plainSummary(t, freshSetup())
	for _, want := range []string{
		"Skills        ✓ 8 linked into claude, agents",
		"+ new  a, b, c, d, e, f, and 2 more",
		"Instructions  ✓ linked into claude, codex",
		"+ new  claude",
		"• backed up  claude (CLAUDE.md.backup-20261116120000)",
		"Sub-agents    ✓ 1 rendered for claude, codex, opencode",
		"~ re-rendered  senior (codex)",
		"○ 1 off in opencode",
		"! 1 conflict, left alone",
		"skill x · claude  /h/.claude/skills/x exists",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("a buffer got ANSI codes:\n%q", out)
	}
}

func TestStackSummaryOfARemoval(t *testing.T) {
	out := plainSummary(t, []stack.Row{
		row("skill", "a", "claude", "removed", "removed"),
		row("instructions", "AGENTS.md", "claude", "restored CLAUDE.md.backup-1", "restored"),
		row("instructions", "AGENTS.md", "codex", "removed", "removed"),
	})
	if strings.Contains(out, "✓") || !strings.Contains(out, "− removed  a") || !strings.Contains(out, "• restored  claude (CLAUDE.md.backup-1)") || !strings.Contains(out, "− removed  codex") {
		t.Errorf("summary:\n%s", out)
	}
}

func TestStackStatusIsAMatrixWithAFixHint(t *testing.T) {
	var buf bytes.Buffer
	err := ui.Print(&buf, StackStatus([]stack.Row{
		row("skill", "tdd", "claude", "linked", ""),
		row("skill", "tdd", "agents", "missing", ""),
		row("skill", "old", "claude", "dangling", ""),
		row("agent", "senior", "claude", "stale", ""),
		row("agent", "senior", "opencode", "on, current", ""),
		row("agent", "junior", "opencode", "off, current", ""),
	}))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Skills       claude       agents",
		"  tdd        ✓ linked     ✗ missing",
		"  old        − dangling",
		"\n\nSub-agents   claude       opencode",
		"  senior     ~ stale      ✓ on",
		"  junior                  ○ off",
		"run `sifatul-stack update`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

func TestStackStatusWithEverythingInPlaceHasNoHint(t *testing.T) {
	var buf bytes.Buffer
	ui.Print(&buf, StackStatus([]stack.Row{row("skill", "tdd", "claude", "linked", "")}))
	if strings.Contains(buf.String(), "sifatul-stack update") {
		t.Errorf("status:\n%s", buf.String())
	}
}
