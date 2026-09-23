package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// frameChrome is what a frame costs its body: a border column and a pad column
// on each side, and a border row top and bottom.
const (
	frameCols = 4
	frameRows = 2
)

// frame draws a rounded box of the given total width around body, with title
// inset in the top edge.
//
// It builds the border by hand rather than through a lipgloss border style,
// because lipgloss draws a plain edge and offers no way to write a title into
// it. Both the title and the body keep whatever styling they arrived with, so
// the caller styles the title, not frame.
//
// Copied from genrefix (and clusage before it), so the tools draw alike.
func frame(title, body string, width int) string {
	if width < frameCols+2 {
		return body
	}
	inner := width - frameCols

	var b strings.Builder
	b.WriteString(dimStyle.Render("╭─ ") + title + " ")
	// 3 for the "╭─ " lead, 1 for the space after the title, 1 for the corner.
	if rule := width - lipgloss.Width(title) - 5; rule > 0 {
		b.WriteString(dimStyle.Render(strings.Repeat("─", rule)))
	}
	b.WriteString(dimStyle.Render("╮") + "\n")

	edge := dimStyle.Render("│")
	// MaxWidth counts columns rather than bytes and keeps the escape sequences,
	// so a long line loses its tail instead of pushing the right border out.
	fit := lipgloss.NewStyle().MaxWidth(inner)
	for _, line := range strings.Split(body, "\n") {
		line = fit.Render(line)
		// Measure with lipgloss, not len: a styled line carries escape bytes
		// that take no columns, and a title can hold a wide rune.
		pad := inner - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(edge + " " + line + strings.Repeat(" ", pad) + " " + edge + "\n")
	}

	b.WriteString(dimStyle.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return b.String()
}

// box is a frame of a fixed total height, so two frames side by side line up.
// A body taller than the box loses its tail. Callers that scroll window their
// own rows first, so only static text ever reaches the cut.
func box(title, body string, width, height int) string {
	rows := max(height-frameRows, 1)
	lines := strings.Split(body, "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return frame(title, strings.Join(lines, "\n"), width)
}

// pad right-pads a styled string to a column width, or truncates it.
//
// It measures with lipgloss rather than len, because a styled cell carries
// escape bytes that take no columns.
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}
