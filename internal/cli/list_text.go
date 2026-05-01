package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/integrations"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

// emitListText prints the styled header/data block for the `list` command
// text-mode output. Status lines go to stderr, version data to stdout, so the
// command composes cleanly in shell pipelines.
func emitListText(result *pipeline.ListResult, langName string, flags *listFlags) error {
	if len(result.Versions) == 0 {
		if flags.runtimeVersion != "" {
			ui.PrintWarning("No versions of %s are compatible with %s %s", result.Package, langName, flags.runtimeVersion)
		} else {
			ui.PrintWarning("No versions found for %s", result.Package)
		}
		return nil
	}
	printVersionListWithRuntime(result.Package, langName, result.Versions, result.LatestStable, flags, result.RuntimeConstraints)
	return nil
}

func printVersionListWithRuntime(pkg, langName string, versions []string, latest string, flags *listFlags, constraints map[string]string) {
	total := len(versions)

	// --- Status/header block (stderr) ---------------------------------------
	ui.PrintNewline()

	if flags.runtimeVersion != "" {
		fmt.Fprintf(os.Stderr, "  %s\n",
			ui.StyleInfo.Render(fmt.Sprintf("Runtime: %s %s (filter)", langName, flags.runtimeVersion)))
		ui.PrintNewline()
	}

	fmt.Fprintf(os.Stderr, "  %s %s\n",
		ui.StyleHighlight.Render(pkg),
		ui.StyleDim.Render(fmt.Sprintf("%s · %d versions", langName, total)))

	latestConstraint := ""
	if constraints != nil {
		latestConstraint = constraints[latest]
	}
	if latestConstraint != "" {
		fmt.Fprintf(os.Stderr, "  %s %s %s\n",
			ui.StyleDim.Render("latest"),
			ui.StyleSuccess.Render(latest),
			ui.StyleDim.Render(fmt.Sprintf("(requires %s %s)", langName, latestConstraint)))
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n",
			ui.StyleDim.Render("latest"),
			ui.StyleSuccess.Render(latest))
	}
	ui.PrintNewline()

	// --- Data block (stdout) ------------------------------------------------
	display := make([]string, 0, len(versions))
	for _, v := range versions {
		if v != latest {
			display = append(display, v)
		}
	}

	truncated := 0
	if !flags.all {
		limit := effectiveListLimit(flags)
		if len(display) > limit {
			truncated = len(display) - limit
			display = display[:limit]
		}
	}

	// When piped or redirected, emit a plain one-version-per-line stream so
	// `stacktower list ... | head` behaves predictably. In a TTY, keep the
	// styled column layout for humans.
	if !ui.StdoutIsTTY() {
		for _, v := range display {
			fmt.Fprintln(os.Stdout, v)
		}
	} else if flags.supportedRuntimes && constraints != nil {
		printVersionColumnsWithRuntime(display, constraints, langName)
	} else {
		printVersionColumns(display)
	}

	if truncated > 0 {
		ui.PrintNewline()
		ui.PrintDetail("… %d older versions not shown (use --all to list all)", truncated)
	}
	ui.PrintNewline()
}

func printVersionColumnsWithRuntime(versions []string, constraints map[string]string, langName string) {
	if len(versions) == 0 {
		return
	}

	maxVerLen := 0
	for _, v := range versions {
		if len(v) > maxVerLen {
			maxVerLen = len(v)
		}
	}

	maxConstraintLen := 0
	for _, v := range versions {
		c := constraints[v]
		if len(c) > maxConstraintLen {
			maxConstraintLen = len(c)
		}
	}

	const indent = 2
	for _, v := range versions {
		fmt.Fprint(os.Stdout, strings.Repeat(" ", indent))

		padded := fmt.Sprintf("%-*s", maxVerLen, v)
		sv := integrations.ParseSemver(v)
		switch {
		case !sv.Valid || sv.Prerelease != "":
			fmt.Fprint(os.Stdout, ui.StyleDim.Render(padded))
		default:
			fmt.Fprint(os.Stdout, ui.StyleValue.Render(padded))
		}

		constraint := constraints[v]
		if constraint != "" {
			fmt.Fprintf(os.Stdout, "  %s", ui.StyleDim.Render(fmt.Sprintf("%s %s", langName, constraint)))
		}
		fmt.Fprintln(os.Stdout)
	}
}

func printVersionColumns(versions []string) {
	if len(versions) == 0 {
		return
	}

	maxLen := 0
	for _, v := range versions {
		if len(v) > maxLen {
			maxLen = len(v)
		}
	}

	const indent = 2
	const colGap = 3
	colWidth := maxLen + colGap
	cols := (ui.StdoutWidth() - indent) / colWidth
	if cols < 1 {
		cols = 1
	}

	for i, v := range versions {
		if i%cols == 0 {
			fmt.Fprint(os.Stdout, strings.Repeat(" ", indent))
		}

		padded := fmt.Sprintf("%-*s", maxLen, v)

		sv := integrations.ParseSemver(v)
		switch {
		case !sv.Valid || sv.Prerelease != "":
			fmt.Fprint(os.Stdout, ui.StyleDim.Render(padded))
		default:
			fmt.Fprint(os.Stdout, ui.StyleValue.Render(padded))
		}

		if i%cols == cols-1 || i == len(versions)-1 {
			fmt.Fprintln(os.Stdout)
		} else {
			fmt.Fprint(os.Stdout, strings.Repeat(" ", colGap))
		}
	}
}
