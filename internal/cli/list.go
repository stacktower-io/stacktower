package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/integrations"
	"github.com/matzehuels/stacktower/pkg/pipeline"
)

const (
	defaultListLimit = 20
	termColumns      = 80
)

type listFlags struct {
	noCache           bool
	all               bool
	runtimeVersion    string
	supportedRuntimes bool
}

func (c *CLI) listCommand() *cobra.Command {
	flags := listFlags{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available package versions",
		Long: `List available versions of a package from its registry.

Versions are sorted semantically (newest first). The latest stable
version is highlighted. Pre-release versions are dimmed.

By default only the 20 most recent versions are shown. Use --all to
see every version.

Use --runtime-version to filter versions compatible with a specific
runtime (e.g., Python 3.8). Use --supported-runtimes to display the
runtime constraint for each version.

Examples:
  stacktower list python fastapi
  stacktower list python fastapi --runtime-version 3.8
  stacktower list python fastapi --supported-runtimes
  stacktower list rust serde
  stacktower list javascript react
  stacktower list go github.com/gin-gonic/gin
  stacktower list python fastapi --all`,
	}

	cmd.PersistentFlags().BoolVar(&flags.noCache, "no-cache", false, "bypass cached version data")
	cmd.PersistentFlags().BoolVar(&flags.all, "all", false, "show all versions (default: newest 20)")
	cmd.PersistentFlags().StringVar(&flags.runtimeVersion, "runtime-version", "", "filter versions compatible with runtime (e.g., 3.8 for Python)")
	cmd.PersistentFlags().BoolVar(&flags.supportedRuntimes, "supported-runtimes", false, "show runtime constraints for each version")

	for _, lang := range languages.All {
		cmd.AddCommand(c.listLangCommand(lang, &flags))
	}

	return cmd
}

func (c *CLI) listLangCommand(lang *deps.Language, flags *listFlags) *cobra.Command {
	return &cobra.Command{
		Use:   fmt.Sprintf("%s <package>", lang.Name),
		Short: fmt.Sprintf("List %s package versions", lang.Name),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runList(cmd.Context(), lang, flags, args[0])
		},
	}
}

func (c *CLI) runList(ctx context.Context, lang *deps.Language, flags *listFlags, pkg string) error {
	if err := validatePackageName(pkg); err != nil {
		return err
	}

	cc, err := newCache(flags.noCache)
	if err != nil {
		return err
	}
	defer cc.Close()

	opts := pipeline.ListOptions{
		Language:           lang.Name,
		Package:            pkg,
		RuntimeVersion:     flags.runtimeVersion,
		IncludeConstraints: flags.supportedRuntimes || flags.runtimeVersion != "",
		Refresh:            flags.noCache,
	}

	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Fetching versions for %s...", pkg))
	spinner.Start()

	result, err := pipeline.ListVersions(ctx, cc, opts)
	if err != nil {
		spinner.StopWithError(fmt.Sprintf("Failed to fetch versions for %s", pkg))
		return WrapSystemError(err, fmt.Sprintf("failed to list versions for %s", pkg), "Check the package name and your network connection.")
	}
	spinner.Stop()

	if len(result.Versions) == 0 {
		if flags.runtimeVersion != "" {
			ui.PrintWarning("No versions of %s are compatible with %s %s", pkg, lang.Name, flags.runtimeVersion)
		} else {
			ui.PrintWarning("No versions found for %s", pkg)
		}
		return nil
	}

	printVersionListWithRuntime(result.Package, lang.Name, result.Versions, result.LatestStable, flags, result.RuntimeConstraints)
	return nil
}

// ---------------------------------------------------------------------------
// Display
// ---------------------------------------------------------------------------

func printVersionListWithRuntime(pkg, langName string, versions []string, latest string, flags *listFlags, constraints map[string]string) {
	total := len(versions)

	ui.PrintNewline()

	// Show runtime filter info if specified
	if flags.runtimeVersion != "" {
		fmt.Fprintf(os.Stderr, "  %s\n",
			ui.StyleInfo.Render(fmt.Sprintf("Runtime: %s %s (filter)", langName, flags.runtimeVersion)))
		ui.PrintNewline()
	}

	fmt.Fprintf(os.Stderr, "  %s %s\n",
		ui.StyleHighlight.Render(pkg),
		ui.StyleDim.Render(fmt.Sprintf("%s · %d versions", langName, total)))

	// Show latest with its runtime constraint if available
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

	display := make([]string, 0, len(versions))
	for _, v := range versions {
		if v != latest {
			display = append(display, v)
		}
	}

	truncated := 0
	if !flags.all && len(display) > defaultListLimit {
		truncated = len(display) - defaultListLimit
		display = display[:defaultListLimit]
	}

	if flags.supportedRuntimes && constraints != nil {
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

	// Find max version length
	maxVerLen := 0
	for _, v := range versions {
		if len(v) > maxVerLen {
			maxVerLen = len(v)
		}
	}

	// Find max constraint length
	maxConstraintLen := 0
	for _, v := range versions {
		c := constraints[v]
		if len(c) > maxConstraintLen {
			maxConstraintLen = len(c)
		}
	}

	const indent = 2
	for _, v := range versions {
		fmt.Fprint(os.Stderr, strings.Repeat(" ", indent))

		padded := fmt.Sprintf("%-*s", maxVerLen, v)
		sv := integrations.ParseSemver(v)
		switch {
		case !sv.Valid || sv.Prerelease != "":
			fmt.Fprint(os.Stderr, ui.StyleDim.Render(padded))
		default:
			fmt.Fprint(os.Stderr, ui.StyleValue.Render(padded))
		}

		constraint := constraints[v]
		if constraint != "" {
			fmt.Fprintf(os.Stderr, "  %s", ui.StyleDim.Render(fmt.Sprintf("%s %s", langName, constraint)))
		}
		fmt.Fprintln(os.Stderr)
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
	cols := (termColumns - indent) / colWidth
	if cols < 1 {
		cols = 1
	}

	for i, v := range versions {
		if i%cols == 0 {
			fmt.Fprint(os.Stderr, strings.Repeat(" ", indent))
		}

		padded := fmt.Sprintf("%-*s", maxLen, v)

		sv := integrations.ParseSemver(v)
		switch {
		case !sv.Valid || sv.Prerelease != "":
			fmt.Fprint(os.Stderr, ui.StyleDim.Render(padded))
		default:
			fmt.Fprint(os.Stderr, ui.StyleValue.Render(padded))
		}

		if i%cols == cols-1 || i == len(versions)-1 {
			fmt.Fprintln(os.Stderr)
		} else {
			fmt.Fprint(os.Stderr, strings.Repeat(" ", colGap))
		}
	}
}
