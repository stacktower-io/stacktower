package ui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/matzehuels/stacktower/pkg/pipeline"
)

// =============================================================================
// Resolver Output Styles
// =============================================================================

var (
	styleResolveHeader     = lipgloss.NewStyle().Bold(true).Foreground(ColorPurple)
	styleResolvePkg        = lipgloss.NewStyle().Foreground(ColorWhite)
	styleResolveVersion    = lipgloss.NewStyle().Foreground(ColorGreen)
	styleResolveConstr     = lipgloss.NewStyle().Foreground(ColorYellow)
	styleResolveRequiredBy = lipgloss.NewStyle().Foreground(ColorGray)
	styleResolveDirect     = lipgloss.NewStyle().Foreground(ColorPurple).Bold(true)
	styleResolveDivider    = lipgloss.NewStyle().Foreground(ColorDim)
)

// =============================================================================
// Type Aliases
// =============================================================================

// Type aliases for pipeline types used in UI functions.
// These make the UI code cleaner while keeping the types in the pipeline package.
type (
	ResolveResult = pipeline.ResolveResult
	ResolveEntry  = pipeline.ResolveEntry
)

// =============================================================================
// Resolver Output
// =============================================================================

// WriteResolveOutput writes a formatted resolver output to w.
func WriteResolveOutput(w io.Writer, result ResolveResult, color bool) {
	if len(result.Entries) == 0 {
		fmt.Fprintln(w, "No dependencies resolved.")
		return
	}

	// Calculate column widths
	pkgWidth := len("Package")
	verWidth := len("Resolved")
	constrWidth := len("Constraint")
	reqWidth := len("Required By")

	for _, e := range result.Entries {
		if len(e.Package) > pkgWidth {
			pkgWidth = len(e.Package)
		}
		if len(e.Version) > verWidth {
			verWidth = len(e.Version)
		}
		constr := FormatConstraints(e.Constraints)
		if len(constr) > constrWidth {
			constrWidth = len(constr)
		}
		req := FormatRequiredBy(e.RequiredBy, e.IsDirect, result.RootName)
		if len(req) > reqWidth {
			reqWidth = len(req)
		}
	}

	// Cap widths for readability
	if pkgWidth > 40 {
		pkgWidth = 40
	}
	if constrWidth > 25 {
		constrWidth = 25
	}
	if reqWidth > 35 {
		reqWidth = 35
	}

	// Header
	if color {
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			styleResolveHeader.Render(PadRight("Package", pkgWidth)),
			styleResolveHeader.Render(PadRight("Resolved", verWidth)),
			styleResolveHeader.Render(PadRight("Constraint", constrWidth)),
			styleResolveHeader.Render("Required By"))
	} else {
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			PadRight("Package", pkgWidth),
			PadRight("Resolved", verWidth),
			PadRight("Constraint", constrWidth),
			"Required By")
	}

	// Divider
	divider := strings.Repeat("─", pkgWidth) + "  " +
		strings.Repeat("─", verWidth) + "  " +
		strings.Repeat("─", constrWidth) + "  " +
		strings.Repeat("─", reqWidth)
	if color {
		fmt.Fprintln(w, styleResolveDivider.Render(divider))
	} else {
		fmt.Fprintln(w, divider)
	}

	// Print direct dependencies first
	printedDirect := false
	for _, e := range result.Entries {
		if !e.IsDirect {
			continue
		}
		if !printedDirect && color {
			fmt.Fprintln(w, styleResolveDirect.Render("Direct dependencies"))
			printedDirect = true
		} else if !printedDirect {
			fmt.Fprintln(w, "Direct dependencies")
			printedDirect = true
		}
		writeResolveEntry(w, e, result.RootName, pkgWidth, verWidth, constrWidth, color)
	}

	// Print transitive dependencies
	printedTrans := false
	for _, e := range result.Entries {
		if e.IsDirect {
			continue
		}
		if !printedTrans {
			if printedDirect {
				fmt.Fprintln(w)
			}
			if color {
				fmt.Fprintln(w, styleResolveDivider.Render("Transitive dependencies"))
			} else {
				fmt.Fprintln(w, "Transitive dependencies")
			}
			printedTrans = true
		}
		writeResolveEntry(w, e, result.RootName, pkgWidth, verWidth, constrWidth, color)
	}
}

func writeResolveEntry(w io.Writer, e ResolveEntry, rootName string, pkgW, verW, constrW int, color bool) {
	pkg := Truncate(e.Package, pkgW)
	ver := e.Version
	constr := Truncate(FormatConstraints(e.Constraints), constrW)
	req := FormatRequiredBy(e.RequiredBy, e.IsDirect, rootName)

	if color {
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			styleResolvePkg.Render(PadRight(pkg, pkgW)),
			styleResolveVersion.Render(PadRight(ver, verW)),
			styleResolveConstr.Render(PadRight(constr, constrW)),
			styleResolveRequiredBy.Render(req))
	} else {
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			PadRight(pkg, pkgW),
			PadRight(ver, verW),
			PadRight(constr, constrW),
			req)
	}
}

// FormatConstraints formats a list of constraints for display.
func FormatConstraints(constraints []string) string {
	if len(constraints) == 0 {
		return "*"
	}
	slices.Sort(constraints)
	constraints = slices.Compact(constraints)
	return strings.Join(constraints, ", ")
}

// FormatRequiredBy formats the "required by" field for display.
func FormatRequiredBy(parents []string, isDirect bool, rootName string) string {
	if isDirect {
		return rootName + " (direct)"
	}
	if len(parents) == 0 {
		return "-"
	}
	slices.Sort(parents)
	if len(parents) > 3 {
		return strings.Join(parents[:3], ", ") + fmt.Sprintf(" +%d more", len(parents)-3)
	}
	return strings.Join(parents, ", ")
}

// PadRight pads a string to the right with spaces to reach the given width.
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// Truncate truncates a string to the given max length, adding "..." if needed.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// PrintResolveSummary prints a summary line for resolved dependencies.
func PrintResolveSummary(w io.Writer, result ResolveResult) {
	fmt.Fprintf(w, "%s %s %s %s %s %s %s %s %s\n",
		styleTreeStat.Render("Resolved"),
		styleTreeNum.Render(fmt.Sprintf("%d", result.DirectCnt+result.TransCnt)),
		styleTreeStat.Render("packages"),
		StyleDim.Render("·"),
		styleTreeNum.Render(fmt.Sprintf("%d", result.DirectCnt)),
		styleTreeStat.Render("direct"),
		StyleDim.Render("·"),
		styleTreeNum.Render(fmt.Sprintf("%d", result.TransCnt)),
		styleTreeStat.Render("transitive"))
}
