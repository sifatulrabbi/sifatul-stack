package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
)

var kinds = []struct{ kind, title, verb string }{
	{"skill", "Skills", "linked into"},
	{"instructions", "Instructions", "linked into"},
	{"agent", "Sub-agents", "rendered for"},
}

var changeOrder = []struct {
	change       stack.Change
	glyph, label string
}{
	{stack.DidLink, "+", "new"},
	{stack.DidWrite, "+", "new"},
	{stack.DidRerender, "~", "re-rendered"},
	{stack.DidReplace, "~", "replaced"},
	{stack.DidPrune, "−", "pruned"},
	{stack.DidRemove, "−", "removed"},
}

const listLimit = 6

func safeRows(rows []stack.Row) []stack.Row {
	out := make([]stack.Row, len(rows))
	for i, r := range rows {
		r.Kind, r.Name, r.Harness, r.Detail = ui.Safe(r.Kind), ui.Safe(r.Name), ui.Safe(r.Harness), ui.Safe(r.Detail)
		out[i] = r
	}
	return out
}

func StackSummary(rows []stack.Row) string {
	rows = safeRows(rows)
	var table [][]string
	for _, k := range kinds {
		ofKind := filterRows(rows, func(r stack.Row) bool { return r.Kind == k.kind && r.State != stack.Failed })
		var lines []string
		if live := filterRows(ofKind, func(r stack.Row) bool {
			return !slices.Contains([]stack.Change{stack.DidRemove, stack.DidPrune}, r.Change)
		}); len(live) > 0 {
			l := ui.OK.Render("✓") + " "
			if k.kind != "instructions" {
				l += fmt.Sprintf("%d ", len(unique(live, func(r stack.Row) string { return r.Name })))
			}
			lines = append(lines, l+k.verb+" "+strings.Join(unique(live, harnessOf), ", "))
		}
		for _, c := range changeOrder {
			changed := filterRows(ofKind, func(r stack.Row) bool { return r.Change == c.change })
			if len(changed) == 0 {
				continue
			}
			style := ui.OK
			if c.glyph != "+" {
				style = ui.Warn
			}
			lines = append(lines, style.Render(c.glyph)+" "+c.label+"  "+changedItems(k.kind, ofKind, changed))
		}
		if k.kind == "agent" {
			off := filterRows(ofKind, func(r stack.Row) bool { return r.Enabled != nil && !*r.Enabled })
			for _, key := range unique(off, func(r stack.Row) string { return r.Harness }) {
				lines = append(lines, ui.Muted.Render(offLine(key, len(filterRows(off, func(r stack.Row) bool { return r.Harness == key })))))
			}
		}
		for i, l := range lines {
			title := ""
			if i == 0 {
				title = ui.Bold.Render(k.title)
			}
			table = append(table, []string{title, l})
		}
	}
	return ui.Columns(table, "  ") + failures(rows)
}

func StackStatus(rows []stack.Row) string {
	rows = safeRows(rows)
	var table [][]string
	needsUpdate := false
	for _, k := range kinds {
		ofKind := filterRows(rows, func(r stack.Row) bool { return r.Kind == k.kind })
		if len(ofKind) == 0 {
			continue
		}
		if len(table) > 0 {
			table = append(table, nil)
		}
		harnesses := unique(ofKind, func(r stack.Row) string { return r.Harness })
		header := []string{ui.Bold.Render(k.title)}
		for _, h := range harnesses {
			header = append(header, ui.Muted.Render(stack.HarnessLabel(h)))
		}
		table = append(table, header)
		for _, name := range unique(ofKind, func(r stack.Row) string { return r.Name }) {
			line := []string{"  " + name}
			for _, h := range harnesses {
				i := slices.IndexFunc(ofKind, func(r stack.Row) bool { return r.Name == name && r.Harness == h })
				if i < 0 {
					line = append(line, "")
					continue
				}
				c, fix := cell(ofKind[i])
				needsUpdate = needsUpdate || fix
				line = append(line, c)
			}
			table = append(table, line)
		}
	}
	out := ui.Columns(table, "   ")
	if c := failures(rows); c != "" {
		out += "\n" + c
	}
	if needsUpdate {
		out += "\n" + ui.Muted.Render("run `sifatul-stack update` to fix what is missing, stale, or left over") + "\n"
	}
	return out
}

func cell(r stack.Row) (string, bool) {
	switch {
	case r.State == stack.Failed:
		return ui.Bad.Render("! failed"), false
	case r.State == stack.Missing:
		return ui.Bad.Render("✗ missing"), true
	case r.State == stack.Stale:
		return ui.Warn.Render("~ stale"), true
	case r.State == stack.Dangling || r.State == stack.Orphaned:
		return ui.Warn.Render("− " + string(r.State)), true
	case r.Enabled != nil && !*r.Enabled:
		return ui.Muted.Render("○ off"), false
	case r.Enabled != nil:
		return ui.OK.Render("✓ on"), false
	default:
		return ui.OK.Render("✓ " + string(r.State)), false
	}
}

func failures(rows []stack.Row) string {
	bad := filterRows(rows, func(r stack.Row) bool { return r.State == stack.Failed })
	if len(bad) == 0 {
		return ""
	}
	var table [][]string
	for _, r := range bad {
		table = append(table, []string{"  " + r.Kind + " " + r.Name + ui.Muted.Render(" · "+harnessOf(r)), r.Detail})
	}
	return ui.Bad.Render(fmt.Sprintf("! %d failed", len(bad))) + "\n" + ui.Columns(table, "  ")
}

func changedItems(kind string, all, changed []stack.Row) string {
	var items []string
	if kind == "instructions" {
		for _, r := range changed {
			item := harnessOf(r)
			if r.Detail != "" {
				item += ui.Muted.Render(" (" + r.Detail + ")")
			}
			items = append(items, item)
		}
	} else {
		for _, name := range unique(changed, func(r stack.Row) string { return r.Name }) {
			got := unique(filterRows(changed, func(r stack.Row) bool { return r.Name == name }), harnessOf)
			every := unique(filterRows(all, func(r stack.Row) bool { return r.Name == name }), harnessOf)
			if len(got) < len(every) {
				name += ui.Muted.Render(" (" + strings.Join(got, ", ") + ")")
			}
			items = append(items, name)
		}
	}
	if len(items) > listLimit {
		items = append(items[:listLimit], ui.Muted.Render(fmt.Sprintf("and %d more", len(items)-listLimit)))
	}
	return strings.Join(items, ", ")
}

func offLine(key string, n int) string {
	line := fmt.Sprintf("○ %d off in %s", n, stack.HarnessLabel(key))
	if h, err := stack.Find(key); err == nil && h.SwitchOn != "" {
		line += "; " + h.SwitchOn
	}
	return line
}

func harnessOf(r stack.Row) string { return stack.HarnessLabel(r.Harness) }

func filterRows(rows []stack.Row, keep func(stack.Row) bool) []stack.Row {
	var out []stack.Row
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func unique(rows []stack.Row, key func(stack.Row) string) []string {
	var out []string
	for _, r := range rows {
		if k := key(r); !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}
