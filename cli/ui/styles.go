package ui

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	Bold   = lipgloss.NewStyle().Bold(true)
	Muted  = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	OK     = lipgloss.NewStyle().Foreground(lipgloss.Green)
	Warn   = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	Bad    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Magenta).Bold(true)
	Label  = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Bold(true)
	Figure = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	Fill   = lipgloss.NewStyle().Foreground(lipgloss.BrightBlue)
	Track  = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
)

func Level(used float64) lipgloss.Style {
	switch levelName(used) {
	case "bad":
		return Bad
	case "warn":
		return Warn
	default:
		return Fill
	}
}

func levelName(used float64) string {
	switch {
	case used >= 80:
		return "bad"
	case used >= 50:
		return "warn"
	default:
		return "ok"
	}
}

func Bar(used float64, width int, solidTrack bool) string {
	filled := max(0, min(width, int(used/100*float64(width)+0.5)))
	track := "░"
	if solidTrack {
		track = "█"
	}
	return Level(used).Render(strings.Repeat("█", filled)) + Track.Render(strings.Repeat(track, width-filled))
}

func Columns(rows [][]string, gap string) string {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	var b strings.Builder
	for _, row := range rows {
		for len(row) > 0 && lipgloss.Width(row[len(row)-1]) == 0 {
			row = row[:len(row)-1]
		}
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)) + gap)
			}
		}
		b.WriteString(line.String() + "\n")
	}
	return b.String()
}

func Print(w io.Writer, s string) error {
	_, err := lipgloss.Fprint(w, s)
	return err
}

func Plain(s string) string { return ansi.Strip(s) }
