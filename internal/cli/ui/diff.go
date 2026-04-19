package ui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

var (
	styleDiffAdded   = lipgloss.NewStyle().Foreground(ColorGreen)
	styleDiffRemoved = lipgloss.NewStyle().Foreground(ColorRed)
	styleDiffUpdated = lipgloss.NewStyle().Foreground(ColorYellow)
	styleDiffLabel   = lipgloss.NewStyle().Foreground(ColorGray)
	styleDiffPkg     = lipgloss.NewStyle().Foreground(ColorWhite)
	styleDiffVersion = lipgloss.NewStyle().Foreground(ColorGreen)
	styleDiffOldVer  = lipgloss.NewStyle().Foreground(ColorRed)
	styleDiffWarn    = lipgloss.NewStyle().Foreground(ColorYellow).Bold(true)
)

// WriteDiff renders a diff result to the writer in the styled terminal format.
func WriteDiff(w io.Writer, d *dag.DiffResult) {
	// Header
	header := stylePathTarget.Render(d.After.RootID)
	if d.Before.RootVersion != "" && d.After.RootVersion != "" {
		header += "  " + styleDiffOldVer.Render(d.Before.RootVersion) +
			" " + StyleDim.Render("→") + " " +
			styleDiffVersion.Render(d.After.RootVersion)
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	// Summary line
	fmt.Fprintf(w, "%s %s    %s %s    %s %s    %s %s\n",
		styleDiffAdded.Render("+"),
		styleDiffAdded.Render(fmt.Sprintf("%d added", len(d.Added))),
		styleDiffRemoved.Render("-"),
		styleDiffRemoved.Render(fmt.Sprintf("%d removed", len(d.Removed))),
		styleDiffUpdated.Render("~"),
		styleDiffUpdated.Render(fmt.Sprintf("%d updated", len(d.Updated))),
		StyleDim.Render("="),
		styleDiffLabel.Render(fmt.Sprintf("%d unchanged", d.Unchanged)),
	)

	// Added section
	if len(d.Added) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, styleDiffAdded.Render("Added"))
		for _, e := range d.Added {
			fmt.Fprintf(w, "  %s %s %s\n",
				styleDiffAdded.Render("+"),
				styleDiffPkg.Render(e.ID),
				styleDiffVersion.Render(e.Version),
			)
		}
	}

	// Removed section
	if len(d.Removed) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, styleDiffRemoved.Render("Removed"))
		for _, e := range d.Removed {
			fmt.Fprintf(w, "  %s %s %s\n",
				styleDiffRemoved.Render("-"),
				styleDiffPkg.Render(e.ID),
				styleDiffLabel.Render(e.Version),
			)
		}
	}

	// Updated section
	if len(d.Updated) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, styleDiffUpdated.Render("Updated"))
		for _, u := range d.Updated {
			fmt.Fprintf(w, "  %s %-20s %s %s %s\n",
				styleDiffUpdated.Render("~"),
				styleDiffPkg.Render(u.ID),
				styleDiffOldVer.Render(u.OldVersion),
				StyleDim.Render("→"),
				styleDiffVersion.Render(u.NewVersion),
			)
		}
	}

	// New vulnerabilities
	if len(d.NewVulns) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, styleDiffWarn.Render("New vulnerabilities"))
		for _, v := range d.NewVulns {
			detail := v.Severity + " severity"
			if v.WasSeverity != "" {
				detail += fmt.Sprintf(" (was %s)", v.WasSeverity)
			}
			fmt.Fprintf(w, "  %s %s %s — %s\n",
				styleDiffWarn.Render("⚠"),
				styleDiffPkg.Render(v.ID),
				styleDiffVersion.Render(v.Version),
				styleDiffLabel.Render(detail),
			)
		}
	}
}
