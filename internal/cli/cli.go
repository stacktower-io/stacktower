// Package cli implements the stacktower command-line interface.
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/observability"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// appName is the application name used for directories and display.
	appName = "stacktower"

	// defaultOrderTimeout is the default timeout for optimal ordering search (seconds).
	defaultOrderTimeout = 60
)

// Log levels exported for use in main.go.
const (
	LogDebug = log.DebugLevel
	LogInfo  = log.InfoLevel
)

// =============================================================================
// CLI - Central CLI State
// =============================================================================

// CLI holds shared state for all commands.
//
// NOTE: ui.SetQuiet and observability.Set*Hooks are package-global, so only
// one CLI instance can be active per process. This is fine for a CLI binary
// but means tests that construct multiple CLI values cannot run in parallel
// with different quiet/log configurations. New returns an error on a second
// call to catch accidental double-initialization (e.g. a test constructing
// two CLI values without coordinating globals).
type CLI struct {
	Logger *log.Logger

	pipelineHooks *cliPipelineHooks // registered once; holds the active spinner
}

// cliInitOnce enforces single-process initialization of the global CLI state
// (observability hooks, ui quiet flag). It is exposed as a package-level
// variable so tests that genuinely need a fresh instance can reset it via
// resetCLIForTesting.
var cliInitOnce sync.Once

// New creates a new CLI instance with a default logger.
// Registers observability hooks for pipeline and security events.
//
// Only one CLI instance may be created per process; calling New a second time
// returns an error. The stacktower binary only creates one CLI, and tests that
// need to validate behavior across multiple configurations should do so through
// the single instance rather than constructing several.
func New(w io.Writer, level log.Level) (*CLI, error) {
	c := &CLI{
		Logger: log.NewWithOptions(w, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "15:04:05.00",
			Level:           level,
		}),
	}
	c.pipelineHooks = &cliPipelineHooks{logger: c.Logger}

	initialized := false
	cliInitOnce.Do(func() {
		observability.SetPipelineHooks(c.pipelineHooks)
		observability.SetSecurityHooks(&cliSecurityHooks{logger: c.Logger})
		initialized = true
	})
	if !initialized {
		return nil, fmt.Errorf("cli.New called more than once: stacktower CLI state is process-global")
	}
	return c, nil
}

// resetCLIForTesting is used exclusively by tests that need to re-run New
// after tearing down a previous CLI instance. Not part of the public API.
func resetCLIForTesting() {
	cliInitOnce = sync.Once{}
}

// AttachOrderingSpinner wires a spinner to receive ordering-progress updates
// via the pipeline hook. Pass nil (e.g. in a defer) to detach. The hook only
// ever knows about the ui.Spinner through this seam; the ordering algorithm
// itself stays free of CLI/TTY dependencies.
func (c *CLI) AttachOrderingSpinner(s *ui.Spinner) {
	if c.pipelineHooks != nil {
		c.pipelineHooks.SetOrderingSpinner(s)
	}
}

// SetLogLevel updates the logger's level.
func (c *CLI) SetLogLevel(level log.Level) {
	c.Logger.SetLevel(level)
}

// SetQuiet suppresses non-essential CLI output (success messages, stats, hints).
func (c *CLI) SetQuiet(q bool) {
	ui.SetQuiet(q)
}

// RootCommand creates the root cobra command with all subcommands registered.
func (c *CLI) RootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "stacktower",
		Short:         "Stacktower visualizes dependency graphs as towers",
		Long:          `Stacktower is a CLI tool for visualizing complex dependency graphs as tiered tower structures, making it easier to understand layering and flow.`,
		Version:       buildinfo.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetVersionTemplate(buildinfo.Template())

	// Custom styled help and usage output
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprint(os.Stderr, ui.RenderHelp(cmd))
	})
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprint(os.Stderr, ui.RenderUsage(cmd))
		return nil
	})

	root.AddCommand(c.versionCommand())
	root.AddCommand(c.infoCommand())
	root.AddCommand(c.parseCommand())
	root.AddCommand(c.resolveCommand())
	root.AddCommand(c.listCommand())
	root.AddCommand(c.layoutCommand())
	root.AddCommand(c.visualizeCommand())
	root.AddCommand(c.renderCommand())
	root.AddCommand(c.cacheCommand())
	root.AddCommand(c.pqtreeCommand())
	root.AddCommand(c.githubCommand())
	root.AddCommand(c.completionCommand())
	root.AddCommand(c.whyCommand())
	root.AddCommand(c.statsCommand())
	root.AddCommand(c.diffCommand())
	root.AddCommand(c.sbomCommand())

	return root
}

// =============================================================================
// Options Helpers
// =============================================================================

// setCLIDefaults applies CLI-specific defaults on top of pipeline defaults.
func setCLIDefaults(opts *pipeline.Options) {
	opts.ApplyPreset(pipeline.PresetCLI)
}

// parseFormats parses a comma-separated format string into a slice.
//
// Empty input defaults to a single-element slice containing the SVG format;
// pipeline.ValidateFormats treats empty as valid, but the CLI wants a
// deterministic default so subsequent flag validation has a concrete format
// to check. Callers should still run pipeline.ValidateFormats on the result
// so the CLI and pipeline stay in lockstep about what counts as valid.
func parseFormats(s string) []string {
	if s == "" {
		return []string{pipeline.FormatSVG}
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
