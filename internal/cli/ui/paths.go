package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	stylePathArrow  = lipgloss.NewStyle().Foreground(ColorDim)
	stylePathPkg    = lipgloss.NewStyle().Foreground(ColorWhite)
	stylePathTarget = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	stylePathCount  = lipgloss.NewStyle().Foreground(ColorGray)
)

// WritePaths renders dependency paths to the writer in the styled terminal format.
func WritePaths(w io.Writer, target, version string, paths [][]string, shortestDepth int) {
	header := stylePathTarget.Render(target)
	if version != "" {
		header += " " + styleTreeVersion.Render(version)
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	arrow := stylePathArrow.Render(" → ")
	for _, path := range paths {
		parts := make([]string, len(path))
		for i, id := range path {
			if id == target {
				parts[i] = stylePathTarget.Render(id)
			} else {
				parts[i] = stylePathPkg.Render(id)
			}
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(parts, arrow))
	}

	fmt.Fprintln(w)
	summary := fmt.Sprintf("%d paths found", len(paths))
	if shortestDepth >= 0 {
		summary += fmt.Sprintf(" (shortest: depth %d)", shortestDepth)
	}
	fmt.Fprintln(w, stylePathCount.Render(summary))
}
