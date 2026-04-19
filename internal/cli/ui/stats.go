package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleStatsSection = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite)
	styleStatsLabel   = lipgloss.NewStyle().Foreground(ColorGray)
	styleStatsNum     = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	styleStatsWarn    = lipgloss.NewStyle().Foreground(ColorYellow)
	styleStatsPkg     = lipgloss.NewStyle().Foreground(ColorWhite)
)

// StatsReport holds all data for the stats terminal output.
type StatsReport struct {
	Root     string
	Version  string
	Language string

	// Overview
	TotalPackages int
	TotalEdges    int
	MaxDepth      int
	DirectDeps    int
	TransitiveDeps int

	// Maintenance
	SingleMaintainerCount int
	SingleMaintainerPct   float64
	Brittle               []string
	Archived              []string
	MedianLastCommitDays  int
	HasMaintenanceData    bool

	// Licenses
	LicenseSummary   map[string]int // risk category -> count
	LicenseBreakdown map[string]int // license name -> count
	Compliant        bool
	HasLicenseData   bool

	// Vulnerabilities
	VulnCritical int
	VulnHigh     int
	VulnMedium   int
	VulnLow      int
	VulnAffected []VulnAffectedPkg
	HasVulnData  bool

	// Load-bearing
	LoadBearing []LoadBearingEntry
}

// VulnAffectedPkg describes a package with a vulnerability.
type VulnAffectedPkg struct {
	Package  string
	Severity string
}

// LoadBearingEntry records a load-bearing package and its reverse dep count.
type LoadBearingEntry struct {
	Package     string
	ReverseDeps int
}

// WriteStats renders a stats report to the writer in the styled terminal format.
func WriteStats(w io.Writer, r StatsReport) {
	// Header
	header := stylePathTarget.Render(r.Root)
	if r.Version != "" {
		header += " " + styleTreeVersion.Render(r.Version)
	}
	if r.Language != "" {
		header += "  " + StyleDim.Render("·") + "  " + styleStatsLabel.Render(r.Language)
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	// Overview
	writeSection(w, "Overview")
	fmt.Fprintf(w, "  %s packages · %s edges · depth %s\n",
		styleStatsNum.Render(fmt.Sprintf("%d", r.TotalPackages)),
		styleStatsNum.Render(fmt.Sprintf("%d", r.TotalEdges)),
		styleStatsNum.Render(fmt.Sprintf("%d", r.MaxDepth)),
	)
	fmt.Fprintf(w, "  %s direct · %s transitive\n",
		styleStatsNum.Render(fmt.Sprintf("%d", r.DirectDeps)),
		styleStatsNum.Render(fmt.Sprintf("%d", r.TransitiveDeps)),
	)

	// Maintenance
	if r.HasMaintenanceData {
		fmt.Fprintln(w)
		writeSection(w, "Maintenance")
		if r.SingleMaintainerCount > 0 {
			fmt.Fprintf(w, "  %s single-maintainer packages (%s%%)\n",
				styleStatsNum.Render(fmt.Sprintf("%d", r.SingleMaintainerCount)),
				fmt.Sprintf("%.0f", r.SingleMaintainerPct),
			)
		}
		if len(r.Brittle) > 0 {
			fmt.Fprintf(w, "  %s brittle packages: %s\n",
				styleStatsWarn.Render(fmt.Sprintf("%d", len(r.Brittle))),
				styleStatsPkg.Render(strings.Join(r.Brittle, ", ")),
			)
		}
		if len(r.Archived) > 0 {
			fmt.Fprintf(w, "  %s archived: %s\n",
				styleStatsWarn.Render(fmt.Sprintf("%d", len(r.Archived))),
				styleStatsPkg.Render(strings.Join(r.Archived, ", ")),
			)
		}
		if r.MedianLastCommitDays > 0 {
			fmt.Fprintf(w, "  Median last commit: %s days ago\n",
				styleStatsNum.Render(fmt.Sprintf("%d", r.MedianLastCommitDays)),
			)
		}
	}

	// Licenses
	if r.HasLicenseData {
		fmt.Fprintln(w)
		writeSection(w, "Licenses")
		for _, cat := range []string{"permissive", "weak-copyleft", "copyleft", "proprietary", "unknown"} {
			count := r.LicenseSummary[cat]
			if count == 0 {
				continue
			}
			// Build breakdown string for this category
			var detail string
			if cat == "permissive" {
				detail = buildLicenseDetail(r.LicenseBreakdown, isPermissiveLicense)
			}
			line := fmt.Sprintf("  %s %s",
				styleStatsNum.Render(fmt.Sprintf("%d", count)),
				styleStatsLabel.Render(cat),
			)
			if detail != "" {
				line += " " + StyleDim.Render("("+detail+")")
			}
			fmt.Fprintln(w, line)
		}
	}

	// Vulnerabilities
	if r.HasVulnData {
		fmt.Fprintln(w)
		writeSection(w, "Vulnerabilities")
		fmt.Fprintf(w, "  %s critical · %s high · %s medium · %s low\n",
			styleStatsNum.Render(fmt.Sprintf("%d", r.VulnCritical)),
			styleStatsNum.Render(fmt.Sprintf("%d", r.VulnHigh)),
			styleStatsNum.Render(fmt.Sprintf("%d", r.VulnMedium)),
			styleStatsNum.Render(fmt.Sprintf("%d", r.VulnLow)),
		)
		if len(r.VulnAffected) > 0 {
			parts := make([]string, len(r.VulnAffected))
			for i, v := range r.VulnAffected {
				parts[i] = fmt.Sprintf("%s (%s)", v.Package, v.Severity)
			}
			fmt.Fprintf(w, "  Affected: %s\n", styleStatsPkg.Render(strings.Join(parts, ", ")))
		}
	}

	// Load-bearing
	if len(r.LoadBearing) > 0 {
		fmt.Fprintln(w)
		writeSection(w, "Top load-bearing packages (most reverse dependencies)")
		limit := 5
		if len(r.LoadBearing) < limit {
			limit = len(r.LoadBearing)
		}
		for i := 0; i < limit; i++ {
			lb := r.LoadBearing[i]
			fmt.Fprintf(w, "  %s. %-20s — %s dependents\n",
				styleStatsNum.Render(fmt.Sprintf("%d", i+1)),
				styleStatsPkg.Render(lb.Package),
				styleStatsNum.Render(fmt.Sprintf("%d", lb.ReverseDeps)),
			)
		}
	}
}

func writeSection(w io.Writer, title string) {
	fmt.Fprintln(w, styleStatsSection.Render(title))
}

func buildLicenseDetail(breakdown map[string]int, filter func(string) bool) string {
	type entry struct {
		name  string
		count int
	}
	var entries []entry
	for name, count := range breakdown {
		if filter(name) {
			entries = append(entries, entry{name, count})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%s: %d", e.name, e.count)
	}
	return strings.Join(parts, ", ")
}

func isPermissiveLicense(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"mit", "isc", "bsd", "apache", "unlicense", "cc0"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
