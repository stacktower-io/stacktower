// Package cli implements the stacktower command-line interface.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/observability"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
	"github.com/stacktower-io/stacktower/pkg/security"
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
type CLI struct {
	Logger *log.Logger
	Quiet  bool // suppress non-essential output (success messages, stats, next steps)
}

// New creates a new CLI instance with a default logger.
// Registers observability hooks for pipeline and security events.
func New(w io.Writer, level log.Level) *CLI {
	c := &CLI{
		Logger: log.NewWithOptions(w, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "15:04:05.00",
			Level:           level,
		}),
	}
	observability.SetPipelineHooks(&cliPipelineHooks{logger: c.Logger})
	observability.SetSecurityHooks(&cliSecurityHooks{logger: c.Logger})
	return c
}

// SetLogLevel updates the logger's level.
func (c *CLI) SetLogLevel(level log.Level) {
	c.Logger.SetLevel(level)
}

// SetQuiet suppresses non-essential CLI output (success messages, stats, hints).
func (c *CLI) SetQuiet(q bool) {
	c.Quiet = q
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

	// Register all subcommands
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
// Runner Factory
// =============================================================================

// newRunner creates a pipeline runner for CLI use.
// When securityScan is true, an OSV-backed vulnerability scanner is attached.
func (c *CLI) newRunner(noCache bool, securityScan bool) (*pipeline.Runner, error) {
	cc, err := newCache(noCache)
	if err != nil {
		return nil, err
	}

	var scanner security.Scanner
	if securityScan {
		scanner = security.NewOSVScanner(nil) // default HTTP client
	}

	return pipeline.NewRunnerWithScanner(cc, nil, c.Logger, scanner), nil
}

func newCache(noCache bool) (cache.Cache, error) {
	if noCache {
		return cache.NewNullCache(), nil
	}
	dir, err := cacheDir()
	if err != nil {
		return cache.NewNullCache(), nil
	}
	fc, err := cache.NewFileCache(dir)
	if err != nil {
		return nil, err
	}
	return cache.NewInstrumentedCache(fc), nil
}

// =============================================================================
// Paths
// =============================================================================

// cacheDir returns the cache directory using XDG standard (~/.cache/stacktower/).
func cacheDir() (string, error) {
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return filepath.Join(cacheHome, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", appName), nil
}

// =============================================================================
// Options Helpers
// =============================================================================

// setCLIDefaults applies CLI-specific defaults on top of pipeline defaults.
func setCLIDefaults(opts *pipeline.Options) {
	opts.ApplyPreset(pipeline.PresetCLI)
}

// parseFormats parses a comma-separated format string into a slice.
func parseFormats(s string) []string {
	if s == "" {
		return []string{pipeline.FormatSVG}
	}
	return strings.Split(s, ",")
}

// =============================================================================
// Shared Parse Pipeline
// =============================================================================

// parseResult holds the output of a parse pipeline run.
type parseResult struct {
	Graph          *dag.DAG
	CacheHit       bool
	RuntimeVersion string // Target runtime version used (e.g., "3.11")
	RuntimeSource  string // Where runtime came from: "cli", "manifest", "default"
}

// runParseWithProgress creates a runner, starts a progress view, runs ParseWithCacheInfo,
// and stops the progress view. This is the shared entrypoint used by both `parse` and `resolve`.
func (c *CLI) runParseWithProgress(ctx context.Context, opts pipeline.Options, noCache, securityScan bool, progressMsg string, maxNodes int) (*parseResult, error) {
	runner, err := c.newRunner(noCache, securityScan)
	if err != nil {
		return nil, fmt.Errorf("initialize runner: %w", err)
	}
	defer runner.Close()

	opts.GitHubToken = getGitHubToken(ctx)
	opts.SecurityScan = securityScan

	// Warn about slower parsing when fetching contributors
	if opts.FetchContributors {
		ui.PrintInfo("Fetching GitHub contributors (this may be slower)")
	}

	pv := ui.NewProgressView(ctx, progressMsg, maxNodes)

	pvLogger := log.New(ui.NewProgressWriter(pv, os.Stderr))
	pvLogger.SetLevel(c.Logger.GetLevel())
	opts.Logger = pvLogger

	pv.Start()

	result, err := runner.ParseWithCacheInfo(ctx, opts)
	if err != nil {
		pv.StopWithError("Failed to resolve dependencies")
		return nil, err
	}
	pv.Stop()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return &parseResult{
		Graph:          result.Graph,
		CacheHit:       result.CacheHit,
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
	}, nil
}

// =============================================================================
// CLI Observability Hook Implementations
// =============================================================================

// cliPipelineHooks logs pipeline stage transitions.
type cliPipelineHooks struct {
	logger *log.Logger
}

func (h *cliPipelineHooks) OnParseStart(_ context.Context, language, pkg string) {
	h.logger.Debug("parse starting", "language", language, "package", pkg)
}

func (h *cliPipelineHooks) OnParseComplete(_ context.Context, language, pkg string, nodes int, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("parse failed", "language", language, "package", pkg, "duration", d, "err", err)
	} else {
		h.logger.Debug("parse complete", "language", language, "package", pkg, "nodes", nodes, "duration", d)
	}
}

func (h *cliPipelineHooks) OnLayoutStart(_ context.Context, vizType string, nodes int) {
	h.logger.Debug("layout starting", "type", vizType, "nodes", nodes)
}

func (h *cliPipelineHooks) OnLayoutComplete(_ context.Context, vizType string, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("layout failed", "type", vizType, "duration", d, "err", err)
	} else {
		h.logger.Debug("layout complete", "type", vizType, "duration", d)
	}
}

func (h *cliPipelineHooks) OnOrderingStart(_ context.Context, algorithm string, rowCount int) {
	h.logger.Debug("ordering starting", "algorithm", algorithm, "rows", rowCount)
}

func (h *cliPipelineHooks) OnOrderingProgress(_ context.Context, explored, pruned, bestCrossings int) {
	h.logger.Debug("ordering progress", "explored", explored, "pruned", pruned, "best_crossings", bestCrossings)
}

func (h *cliPipelineHooks) OnOrderingComplete(_ context.Context, crossings int, d time.Duration) {
	h.logger.Debug("ordering complete", "crossings", crossings, "duration", d)
}

func (h *cliPipelineHooks) OnRenderStart(_ context.Context, formats []string) {
	h.logger.Debug("render starting", "formats", formats)
}

func (h *cliPipelineHooks) OnRenderComplete(_ context.Context, formats []string, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("render failed", "formats", formats, "duration", d, "err", err)
	} else {
		h.logger.Debug("render complete", "formats", formats, "duration", d)
	}
}

// cliSecurityHooks provides user-facing feedback during vulnerability scanning.
type cliSecurityHooks struct {
	logger *log.Logger
}

func (h *cliSecurityHooks) OnScanStart(_ context.Context, ecosystem string, depCount int) {
	ui.PrintInfo("Scanning %d %s dependencies for vulnerabilities...", depCount, ecosystem)
}

func (h *cliSecurityHooks) OnScanComplete(_ context.Context, ecosystem string, findings int, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("security scan failed", "ecosystem", ecosystem, "duration", d, "err", err)
	} else if findings > 0 {
		ui.PrintWarning("Found %d vulnerabilities (%s)", findings, ui.FormatDuration(d))
	} else {
		ui.PrintInfo("No known vulnerabilities found (%s)", ui.FormatDuration(d))
	}
}
