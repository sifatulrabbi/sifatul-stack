package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

func row(kind, name, harness string, state stack.State, change stack.Change) stack.Row {
	return stack.Row{Kind: kind, Name: name, Harness: harness, State: state, Change: change}
}

func toggled(name string, enabled bool) stack.Row {
	return stack.Row{Kind: "agent", Name: name, Harness: "opencode", State: stack.Current, Enabled: &enabled}
}

func freshSetup() []stack.Row {
	var rows []stack.Row
	for _, sk := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		rows = append(rows, row("skill", sk, "claude", "linked", "linked"), row("skill", sk, "agents", "linked", "linked"))
	}
	rows = append(rows,
		stack.Row{Kind: "instructions", Name: "AGENTS.md", Harness: "claude", State: stack.BackedUp, Change: stack.DidBackUp, Detail: "CLAUDE.md.backup-20261116120000"},
		row("instructions", "AGENTS.md", "claude", "linked", "linked"),
		row("instructions", "AGENTS.md", "codex", "linked", ""),
		row("agent", "senior", "claude", "current", ""),
		row("agent", "senior", "codex", "written", "re-rendered"),
		toggled("senior", false),
		stack.Row{Kind: "skill", Name: "x", Harness: "claude", State: stack.Conflict, Detail: "/h/.claude/skills/x exists"},
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
		"Skills        ✓ 8 linked into Claude Code, ~/.agents",
		"+ new  a, b, c, d, e, f, and 2 more",
		"Instructions  ✓ linked into Claude Code, Codex",
		"+ new  Claude Code",
		"• backed up  Claude Code (CLAUDE.md.backup-20261116120000)",
		"Sub-agents    ✓ 1 rendered for Claude Code, Codex, OpenCode",
		"~ re-rendered  senior (Codex)",
		"○ 1 off in OpenCode",
		"! 1 conflict, left alone",
		"skill x · Claude Code  /h/.claude/skills/x exists",
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
		stack.Row{Kind: "instructions", Name: "AGENTS.md", Harness: "claude", State: stack.Restored, Change: stack.DidRestore, Detail: "CLAUDE.md.backup-1"},
		row("instructions", "AGENTS.md", "codex", "removed", "removed"),
	})
	if strings.Contains(out, "✓") || !strings.Contains(out, "− removed  a") || !strings.Contains(out, "• restored  Claude Code (CLAUDE.md.backup-1)") || !strings.Contains(out, "− removed  Codex") {
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
		toggled("senior", true),
		toggled("junior", false),
	}))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Skills       Claude Code   ~/.agents",
		"  tdd        ✓ linked      ✗ missing",
		"  old        − dangling",
		"\n\nSub-agents   Claude Code   OpenCode",
		"  senior     ~ stale       ✓ on",
		"  junior                   ○ off",
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

func TestStackOutputStripsEscapesFromRepoNames(t *testing.T) {
	evil := "x" + "\x1b]52;c;ZWNobyBwd25lZA==\x07"
	rows := []stack.Row{
		row("skill", evil, "claude", stack.Linked, stack.DidLink),
		row("skill", evil+"2", "claude", stack.Missing, ""),
		{Kind: "instructions", Name: "AGENTS.md", Harness: "claude", State: stack.BackedUp, Change: stack.DidBackUp, Detail: "CLAUDE.md.backup-1\x07"},
		{Kind: "skill", Name: evil, Harness: "agents", State: stack.Conflict, Detail: "/h/" + evil},
	}
	for name, out := range map[string]string{"summary": StackSummary(rows), "status": StackStatus(rows)} {
		for _, bad := range []string{"\x1b]", "\x07"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s carries %q:\n%q", name, bad, out)
			}
		}
	}
}
