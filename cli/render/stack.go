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

var changeOrder = []struct{ change, glyph, label string }{
	{"linked", "+", "new"},
	{"written", "+", "new"},
	{"re-rendered", "~", "re-rendered"},
	{"backed up", "•", "backed up"},
	{"restored", "•", "restored"},
	{"pruned", "−", "pruned"},
	{"removed", "−", "removed"},
}

const listLimit = 6

func StackSummary(rows []stack.Row) string {
	var table [][]string
	for _, k := range kinds {
		ofKind := filterRows(rows, func(r stack.Row) bool { return r.Kind == k.kind && !r.Conflict })
		var lines []string
		if live := filterRows(ofKind, func(r stack.Row) bool { return !slices.Contains([]string{"removed", "pruned", "restored"}, r.Change) }); len(live) > 0 {
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
			off := filterRows(ofKind, func(r stack.Row) bool { return strings.HasPrefix(r.State, "off,") })
			if len(off) > 0 {
				lines = append(lines, ui.Muted.Render(fmt.Sprintf("○ %d off in opencode; delete `disabled: true` from one to switch it on", len(off))))
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
	return ui.Columns(table, "  ") + conflicts(rows)
}

func StackStatus(rows []stack.Row) string {
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
		harnesses := unique(ofKind, harnessOf)
		header := []string{ui.Bold.Render(k.title)}
		for _, h := range harnesses {
			header = append(header, ui.Muted.Render(h))
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
	if c := conflicts(rows); c != "" {
		out += "\n" + c
	}
	if needsUpdate {
		out += "\n" + ui.Muted.Render("run `sifatul-stack update` to fix what is missing, stale, or left over") + "\n"
	}
	return out
}

func cell(r stack.Row) (string, bool) {
	state, prefix := r.State, ""
	if on, rest, ok := strings.Cut(state, ", "); ok && (on == "on" || on == "off") {
		state, prefix = rest, on
	}
	switch {
	case r.Conflict:
		return ui.Bad.Render("! conflict"), false
	case strings.HasPrefix(state, "missing"):
		return ui.Bad.Render("✗ missing"), true
	case state == "stale":
		return ui.Warn.Render("~ stale"), true
	case state == "dangling" || state == "orphaned":
		return ui.Warn.Render("− " + state), true
	case prefix == "off":
		return ui.Muted.Render("○ off"), false
	case prefix == "on":
		return ui.OK.Render("✓ on"), false
	default:
		return ui.OK.Render("✓ " + state), false
	}
}

func conflicts(rows []stack.Row) string {
	bad := filterRows(rows, func(r stack.Row) bool { return r.Conflict })
	if len(bad) == 0 {
		return ""
	}
	noun := "conflict"
	if len(bad) > 1 {
		noun += "s"
	}
	var table [][]string
	for _, r := range bad {
		table = append(table, []string{"  " + r.Kind + " " + r.Name + ui.Muted.Render(" · "+r.Harness), strings.TrimPrefix(r.State, "conflict: ")})
	}
	return ui.Bad.Render(fmt.Sprintf("! %d %s, left alone", len(bad), noun)) + "\n" + ui.Columns(table, "  ")
}

func changedItems(kind string, all, changed []stack.Row) string {
	var items []string
	if kind == "instructions" {
		for _, r := range changed {
			item := r.Harness
			if r.Change == "backed up" || r.Change == "restored" {
				f := strings.Fields(r.State)
				item += ui.Muted.Render(" (" + f[len(f)-1] + ")")
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

func harnessOf(r stack.Row) string { return r.Harness }

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
