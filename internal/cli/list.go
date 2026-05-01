package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

const defaultListLimit = 20

// effectiveListLimit resolves the user-facing display limit. Users can pass
// --limit to override the default of 20; --all wins over --limit entirely
// (handled at the call site). A zero or negative value falls back to the
// default so the flag stays forgiving.
func effectiveListLimit(flags *listFlags) int {
	if flags.limit > 0 {
		return flags.limit
	}
	return defaultListLimit
}

type listFlags struct {
	noCache           bool
	all               bool
	limit             int
	runtimeVersion    string
	supportedRuntimes bool
	format            string
	output            string
}

// listJSONOutput is the shape returned by `stacktower list ... -f json`.
type listJSONOutput struct {
	Package            string            `json:"package"`
	Language           string            `json:"language"`
	LatestStable       string            `json:"latest_stable,omitempty"`
	Versions           []string          `json:"versions"`
	RuntimeConstraints map[string]string `json:"runtime_constraints,omitempty"`
	RuntimeFilter      string            `json:"runtime_filter,omitempty"`
	Truncated          int               `json:"truncated,omitempty"`
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

Output:
  Version strings are written to stdout (one per line when piped, as a
  padded grid in a TTY). Status messages and decoration go to stderr,
  so scripts can pipe cleanly:

      stacktower list python fastapi | head

Examples:
  stacktower list python fastapi
  stacktower list python fastapi --runtime-version 3.8
  stacktower list python fastapi --supported-runtimes
  stacktower list python fastapi --all
  stacktower list python fastapi -f json
  stacktower list rust serde
  stacktower list javascript react
  stacktower list go github.com/gin-gonic/gin`,
	}

	cmd.PersistentFlags().BoolVar(&flags.noCache, "no-cache", false, "bypass cached version data")
	cmd.PersistentFlags().BoolVar(&flags.all, "all", false, "show all versions (default: newest 20)")
	cmd.PersistentFlags().IntVar(&flags.limit, "limit", 0, "limit the number of versions shown (default: 20; ignored when --all is set)")
	cmd.PersistentFlags().StringVar(&flags.runtimeVersion, "runtime-version", "", "filter versions compatible with runtime (e.g., 3.8 for Python)")
	cmd.PersistentFlags().BoolVar(&flags.supportedRuntimes, "supported-runtimes", false, "show runtime constraints for each version")
	cmd.PersistentFlags().StringVarP(&flags.format, "format", "f", "", "output format: text (default), json")
	cmd.PersistentFlags().StringVarP(&flags.output, "output", "o", "", "write output to file (json format only; default: stdout)")

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
	if flags.limit < 0 {
		return NewUserError(
			"invalid --limit value",
			"--limit must be a positive integer (e.g. --limit 50). Omit the flag for the default of 20.",
		)
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

	// Status UI always goes to stderr so stdout stays clean for pipes.
	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Fetching versions for %s...", pkg))
	spinner.Start()

	result, err := pipeline.ListVersions(ctx, cc, opts)
	if err != nil {
		spinner.StopWithError(fmt.Sprintf("Failed to fetch versions for %s", pkg))
		return WrapSystemError(err, fmt.Sprintf("failed to list versions for %s", pkg), "Check the package name and your network connection.")
	}
	spinner.Stop()

	switch flags.format {
	case FormatJSON:
		return emitListJSON(result, lang.Name, flags)
	case "", FormatText:
		if flags.output != "" {
			return NewUserError(
				"-o/--output is only supported with -f json",
				"Use `-f json -o file.json` to save structured output, or drop -o for text output.",
			)
		}
		return emitListText(result, lang.Name, flags)
	default:
		return unsupportedFormatError(flags.format, nil)
	}
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

func emitListJSON(result *pipeline.ListResult, langName string, flags *listFlags) error {
	out := listJSONOutput{
		Package:            result.Package,
		Language:           langName,
		LatestStable:       result.LatestStable,
		Versions:           result.Versions,
		RuntimeConstraints: result.RuntimeConstraints,
		RuntimeFilter:      flags.runtimeVersion,
	}
	if !flags.all {
		limit := effectiveListLimit(flags)
		if len(out.Versions) > limit {
			out.Truncated = len(out.Versions) - limit
			out.Versions = out.Versions[:limit]
		}
	}

	if err := writeFormatted(flags.output, FormatJSON, map[string]func(io.Writer) error{
		FormatJSON: func(w io.Writer) error { return encodeJSON(w, out) },
	}); err != nil {
		return err
	}
	if flags.output != "" {
		ui.PrintSuccess("Versions written")
		ui.PrintFile(flags.output)
	}
	return nil
}
