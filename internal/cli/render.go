package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/ordering"
	"github.com/stacktower-io/stacktower/pkg/observability"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

// renderCommand creates the render command for generating visualizations.
func (c *CLI) renderCommand() *cobra.Command {
	var (
		formatsStr   string
		output       string
		noCache      bool
		orderTimeout int
	)
	opts := pipeline.Options{}
	setCLIDefaults(&opts)

	cmd := &cobra.Command{
		Use:   "render [graph.json|-]",
		Short: "Render a dependency graph to SVG/PNG/PDF (shortcut for layout + visualize)",
		Long: `Render a dependency graph to visual output.

This command is a shortcut that combines 'layout' and 'visualize' in one step.
It takes a graph.json file (produced by 'parse') and outputs SVG, PNG, or PDF.
Use '-' as input to read graph JSON from stdin.

Results are cached locally for faster subsequent runs.

If you want to save the intermediate layout, use 'layout' followed by 'visualize'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Formats = parseFormats(formatsStr)
			if err := validateRenderFormats(opts.Formats); err != nil {
				return err
			}
			if err := validateRenderStyle(opts.Style); err != nil {
				return err
			}
			if err := validateOrderingTimeout(orderTimeout); err != nil {
				return err
			}
			return c.runRender(cmd.Context(), args[0], opts, output, noCache, orderTimeout)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (single format) or base path (multiple)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable caching")
	addLayoutFlags(cmd, &opts, &orderTimeout)
	addRenderFlags(cmd, &opts, &formatsStr)
	addSecurityFlags(cmd, &opts)

	return cmd
}

// runRender loads the graph and renders via pipeline.
func (c *CLI) runRender(ctx context.Context, input string, opts pipeline.Options, output string, noCache bool, orderTimeout int) error {
	start := time.Now()

	g, err := loadGraph(input)
	if err != nil {
		if input == "-" {
			return WrapSystemError(err, "failed to read graph from stdin", "Pipe valid graph JSON or pass a graph file path.")
		}
		return WrapSystemError(err, fmt.Sprintf("failed to load graph %s", input), "Check that the file exists and is valid JSON.")
	}

	// Check if Nebraska rankings are requested but contributor data is missing
	if opts.Nebraska && !dagHasContributorData(g) {
		ui.PrintWarning("Graph has no contributor data. Nebraska rankings will be limited.")
		ui.PrintDetail("Re-parse with --contributors flag for accurate maintainer rankings")
	}

	runner, err := c.newRunner(noCache, false)
	if err != nil {
		return WrapSystemError(err, "failed to initialize runner", "This may be a cache or configuration issue.")
	}
	defer runner.Close()

	opts.Logger = c.Logger

	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Rendering %s...", opts.VizType))

	var orderer *optimalOrderer
	if opts.NeedsOptimalOrderer() {
		if o, ok := c.newOptimalOrderer(ctx, orderTimeout).(*optimalOrderer); ok {
			orderer = o
			opts.Orderer = o
		} else {
			opts.Orderer = c.newOptimalOrderer(ctx, orderTimeout)
		}
		// Route ordering-progress events to the spinner via the pipeline hook
		// so the orderer itself doesn't need to know about the CLI UI layer.
		c.AttachOrderingSpinner(spinner)
		defer c.AttachOrderingSpinner(nil)
	}

	spinner.Start()
	spinner.UpdateMessage("Normalizing graph...")

	workGraph, err := runner.PrepareGraph(g, opts)
	if err != nil {
		spinner.StopWithError("Normalization failed")
		return WrapSystemError(err, "graph normalization failed", "The dependency graph may contain invalid structure.")
	}

	spinner.UpdateMessage(fmt.Sprintf("Computing layout (%d nodes)...", workGraph.NodeCount()))

	layout, layoutHit, err := runner.GenerateLayoutWithCacheInfo(ctx, workGraph, opts)
	if err != nil {
		spinner.StopWithError("Render failed")
		return WrapSystemError(err, "layout computation failed", "Try reducing max-nodes or simplifying the graph.")
	}

	if ctx.Err() != nil {
		spinner.Stop()
		return ctx.Err()
	}

	spinner.UpdateMessage(fmt.Sprintf("Rendering %s...", strings.Join(opts.Formats, ", ")))

	artifacts, renderHit, err := runner.RenderWithCacheInfo(ctx, layout, workGraph, opts)
	if err != nil {
		spinner.StopWithError("Render failed")
		return WrapSystemError(err, "rendering failed", "Check the output format and try again.")
	}
	spinner.Stop()

	// Prefer the orderer's live count when we just computed the layout
	// (it may reflect partial/timed-out state more accurately). Otherwise
	// fall back to the value persisted on the layout itself.
	var crossings int
	if orderer != nil && !layoutHit {
		crossings = orderer.crossings
	} else {
		crossings = layout.Crossings
	}
	// "optimal" is the flag value, but the search is bounded (candidate caps,
	// timeout, wide-row fallback) so label the result honestly as "optimized"
	// and qualify how the search ended.
	orderingName := opts.Ordering
	if orderingName == "" || orderingName == "optimal" {
		orderingName = "optimized"
	}
	if orderer != nil && !layoutHit {
		switch {
		case orderer.outcome.Fallback:
			orderingName += " (barycentric fallback: row too wide)"
		case orderer.outcome.TimedOut:
			orderingName += " (timed out, best effort)"
		}
	}
	style := layout.Style
	if style == "" {
		style = "handdrawn"
	}

	return writeArtifacts(artifactWriteParams{
		artifacts: artifacts,
		formats:   opts.Formats,
		input:     input,
		output:    output,
		nodeCount: g.NodeCount(),
		edgeCount: g.EdgeCount(),
		cacheHit:  layoutHit && renderHit,
		elapsed:   time.Since(start),
		renderStats: ui.RenderStats{
			Layers:      len(layout.Rows),
			Crossings:   crossings,
			OrderingRan: true,
			Ordering:    orderingName,
			Style:       style,
		},
	})
}

// =============================================================================
// Optimal Orderer
// =============================================================================

// optimalOrderer wraps ordering.OptimalSearch and surfaces events via
// observability hooks. It deliberately knows nothing about ui.Spinner or
// other CLI affordances: progress is routed through
// observability.Pipeline().OnOrderingProgress, which the CLI's registered
// pipeline hook forwards to the active spinner (see AttachOrderingSpinner).
//
// The caller's ctx is captured at construction time and passed to each
// observability call so tracing/cancellation propagates correctly.
type optimalOrderer struct {
	ordering.OptimalSearch
	cli       *CLI
	ctx       context.Context
	crossings int                  // Last computed crossings count
	outcome   ordering.OutcomeInfo // How the search concluded (timeout/fallback)
	startTime time.Time            // For duration tracking
	rowCount  int                  // Number of rows being ordered
}

// newOptimalOrderer creates an optimal orderer bound to the caller's ctx.
func (c *CLI) newOptimalOrderer(ctx context.Context, timeoutSec int) ordering.Orderer {
	if ctx == nil {
		ctx = context.Background()
	}
	o := &optimalOrderer{cli: c, ctx: ctx}
	o.OptimalSearch = ordering.OptimalSearch{
		Timeout:  time.Duration(timeoutSec) * time.Second,
		Progress: o.onProgress,
		Debug:    o.onDebug,
		Outcome:  func(info ordering.OutcomeInfo) { o.outcome = info },
		// Wire the caller's context so Ctrl-C aborts the search immediately
		// (returning the best ordering found) instead of waiting out the
		// full timeout.
		Ctx: ctx,
	}
	return o
}

func (o *optimalOrderer) onProgress(explored, pruned, bestScore int) {
	if bestScore < 0 {
		return
	}
	observability.Pipeline().OnOrderingProgress(o.ctx, explored, pruned, bestScore)
}

func (o *optimalOrderer) onDebug(info ordering.DebugInfo) {
	o.cli.Logger.Debug("search complete", "rows", info.TotalRows, "depth", info.MaxDepth)
}

// Fingerprint identifies this orderer configuration for layout cache keys.
// Two runs with the same ordering algorithm and timeout may share cached
// layouts; different timeouts may produce different (best-so-far) results.
func (o *optimalOrderer) Fingerprint() string {
	return fmt.Sprintf("optimal:%s", o.Timeout)
}

// OrderRows implements ordering.Orderer.
func (o *optimalOrderer) OrderRows(g *dag.DAG) map[int][]string {
	o.startTime = time.Now()
	o.rowCount = g.RowCount()

	observability.Pipeline().OnOrderingStart(o.ctx, "optimal", o.rowCount)

	result := o.OptimalSearch.OrderRows(g)
	o.crossings = dag.CountCrossings(g, result)

	observability.Pipeline().OnOrderingComplete(o.ctx, o.crossings, time.Since(o.startTime))
	o.cli.Logger.Debug("ordering result",
		"crossings", o.crossings,
		"timedOut", o.outcome.TimedOut,
		"fallback", o.outcome.Fallback)

	return result
}

// =============================================================================
// Artifact Writing
// =============================================================================

// artifactWriteParams configures artifact file writing.
type artifactWriteParams struct {
	artifacts   map[string][]byte
	formats     []string
	input       string
	output      string
	nodeCount   int
	edgeCount   int
	cacheHit    bool
	elapsed     time.Duration
	renderStats ui.RenderStats
}

// writeArtifacts writes rendered artifacts to files and prints a summary.
func writeArtifacts(p artifactWriteParams) error {
	base := deriveBasePath(p.input, p.output)
	var paths []string

	for _, format := range p.formats {
		data, ok := p.artifacts[format]
		if !ok {
			return NewSystemError(
				fmt.Sprintf("missing artifact for format: %s", format),
				"This is an internal error. Please report this issue.",
			)
		}

		path := p.output
		if path == "" || len(p.formats) > 1 {
			path = base + "." + format
		}

		if err := writeFile(data, path); err != nil {
			return err
		}
		paths = append(paths, path)
	}

	if p.renderStats.OrderingRan && p.renderStats.Crossings == 0 {
		ui.PrintSuccess("Render complete (no edge crossings)")
	} else if p.renderStats.Crossings > 0 {
		ui.PrintInfo("Render complete (%d crossings remaining)", p.renderStats.Crossings)
	} else {
		ui.PrintSuccess("Render complete")
	}
	for _, path := range paths {
		ui.PrintFile(path)
	}
	ui.PrintStats(p.nodeCount, p.edgeCount, 0, p.cacheHit, p.elapsed)
	ui.PrintRenderStats(p.renderStats)
	if len(paths) == 1 && strings.HasSuffix(paths[0], ".svg") {
		ui.PrintNewline()
		ui.PrintNextStep("Open", "open "+paths[0])
	}
	return nil
}

// deriveBasePath computes the base path for output files.
//
// When -o is set and its extension matches a known render format, we strip
// the extension so multi-format runs (e.g. -f svg,png) can append each
// format's extension without doubling up ("foo.svg.svg").
//
// When -o is not set, we derive the base from the input (.layout.json) path:
// we trim both the real extension (".json") and a trailing ".layout"
// component, so "foo.layout.json" becomes "foo". This also trims for
// non-.json inputs like "some.layout.svg" → "some"; that's an acceptable
// overreach because layout inputs are almost always .json in practice.
func deriveBasePath(input, output string) string {
	if output != "" {
		ext := filepath.Ext(output)
		if pipeline.IsValidFormat(strings.TrimPrefix(ext, ".")) {
			return strings.TrimSuffix(output, ext)
		}
		return output
	}
	base := strings.TrimSuffix(input, filepath.Ext(input))
	return strings.TrimSuffix(base, ".layout")
}

// dagHasContributorData checks if any node in the DAG has contributor/maintainer data.
func dagHasContributorData(g *dag.DAG) bool {
	for _, n := range g.Nodes() {
		if n.Meta == nil {
			continue
		}
		if maintainers, ok := n.Meta["repo_maintainers"]; ok {
			switch v := maintainers.(type) {
			case []string:
				if len(v) > 0 {
					return true
				}
			case []any:
				if len(v) > 0 {
					return true
				}
			}
		}
	}
	return false
}
