package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/ordering"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/observability"
	"github.com/matzehuels/stacktower/pkg/pipeline"
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
		Use:   "render [graph.json]",
		Short: "Render a dependency graph to SVG/PNG/PDF (shortcut for layout + visualize)",
		Long: `Render a dependency graph to visual output.

This command is a shortcut that combines 'layout' and 'visualize' in one step.
It takes a graph.json file (produced by 'parse') and outputs SVG, PNG, or PDF.

Results are cached locally for faster subsequent runs.

If you want to save the intermediate layout, use 'layout' followed by 'visualize'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Formats = parseFormats(formatsStr)
			if err := pipeline.ValidateFormats(opts.Formats); err != nil {
				return err
			}
			if err := pipeline.ValidateStyle(opts.Style); err != nil {
				return err
			}
			return c.runRender(cmd.Context(), args[0], opts, output, noCache, orderTimeout)
		},
	}

	// Common flags
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (single format) or base path (multiple)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable caching")

	// Layout flags
	cmd.Flags().StringVarP(&opts.VizType, "type", "t", opts.VizType, "visualization type: tower (default), nodelink")
	cmd.Flags().BoolVar(&opts.Normalize, "normalize", opts.Normalize, "apply graph normalization")
	cmd.Flags().Float64Var(&opts.Width, "width", opts.Width, "frame width")
	cmd.Flags().Float64Var(&opts.Height, "height", opts.Height, "frame height")
	cmd.Flags().StringVar(&opts.Ordering, "ordering", opts.Ordering, "ordering algorithm: optimal (default), barycentric")
	cmd.Flags().BoolVar(&opts.Randomize, "randomize", opts.Randomize, "randomize block widths (tower)")
	cmd.Flags().BoolVar(&opts.Merge, "merge", opts.Merge, "merge subdivider blocks (tower)")
	cmd.Flags().BoolVar(&opts.Nebraska, "nebraska", opts.Nebraska, "show Nebraska maintainer ranking (tower)")
	cmd.Flags().IntVar(&orderTimeout, "ordering-timeout", defaultOrderTimeout, "timeout in seconds for optimal ordering search")

	// Render flags
	cmd.Flags().StringVar(&opts.Style, "style", opts.Style, "visual style: handdrawn (default), simple")
	cmd.Flags().BoolVar(&opts.ShowEdges, "edges", opts.ShowEdges, "show dependency edges (tower)")
	cmd.Flags().BoolVar(&opts.Popups, "popups", opts.Popups, "show hover popups with metadata")
	cmd.Flags().StringVarP(&formatsStr, "format", "f", "", "output format(s): svg (default), pdf, png (comma-separated)")

	// Security flags
	cmd.Flags().BoolVar(&opts.ShowVulns, "show-vulns", opts.ShowVulns, "show vulnerability severity colours (requires scanned graph)")
	cmd.Flags().BoolVar(&opts.ShowLicenses, "show-licenses", opts.ShowLicenses, "show license compliance indicators (copyleft/unknown borders)")
	cmd.Flags().BoolVar(&opts.FlagsOnTop, "flags-on-top", opts.FlagsOnTop, "render security flags on top of all blocks")

	return cmd
}

// runRender loads the graph and renders via pipeline.
func (c *CLI) runRender(ctx context.Context, input string, opts pipeline.Options, output string, noCache bool, orderTimeout int) error {
	start := time.Now()

	g, err := graph.ReadGraphFile(input)
	if err != nil {
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
		orderer = c.newOptimalOrderer(orderTimeout).(*optimalOrderer)
		orderer.spinner = spinner // Wire spinner for live updates
		opts.Orderer = orderer
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

	// Get crossings from orderer (computed during layout) or fallback to layout-based count
	var crossings int
	if orderer != nil && !layoutHit {
		crossings = orderer.crossings
	} else {
		crossings = countCrossingsFromLayout(layout)
	}
	orderingName := opts.Ordering
	if orderingName == "" {
		orderingName = "optimal"
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
			Layers:    len(layout.Rows),
			Crossings: crossings,
			Ordering:  orderingName,
			Style:     style,
		},
	})
}

// =============================================================================
// Optimal Orderer
// =============================================================================

// optimalOrderer wraps ordering.OptimalSearch with CLI progress feedback.
type optimalOrderer struct {
	ordering.OptimalSearch
	cli       *CLI
	crossings int         // Last computed crossings count
	spinner   *ui.Spinner // Optional spinner for live updates
	startTime time.Time   // For duration tracking
	rowCount  int         // Number of rows being ordered
}

// newOptimalOrderer creates an optimal orderer with a timeout.
func (c *CLI) newOptimalOrderer(timeoutSec int) ordering.Orderer {
	o := &optimalOrderer{cli: c}
	o.OptimalSearch = ordering.OptimalSearch{
		Timeout:  time.Duration(timeoutSec) * time.Second,
		Progress: o.onProgress,
		Debug:    o.onDebug,
	}
	return o
}

func (o *optimalOrderer) onProgress(explored, pruned, bestScore int) {
	if bestScore >= 0 {
		o.cli.Logger.Debug("search progress", "explored", explored, "pruned", pruned, "crossings", bestScore)

		// Update spinner with live progress
		if o.spinner != nil {
			o.spinner.UpdateMessage(fmt.Sprintf("Ordering... %s explored, best: %d crossings",
				formatCount(explored+pruned), bestScore))
		}

		// Emit observability hook
		observability.Pipeline().OnOrderingProgress(context.Background(), explored, pruned, bestScore)
	}
}

func (o *optimalOrderer) onDebug(info ordering.DebugInfo) {
	o.cli.Logger.Debug("search complete", "rows", info.TotalRows, "depth", info.MaxDepth)
}

// OrderRows implements ordering.Orderer.
func (o *optimalOrderer) OrderRows(g *dag.DAG) map[int][]string {
	o.startTime = time.Now()
	o.rowCount = g.RowCount()

	// Emit start hook
	observability.Pipeline().OnOrderingStart(context.Background(), "optimal", o.rowCount)

	result := o.OptimalSearch.OrderRows(g)
	o.crossings = dag.CountCrossings(g, result)

	// Emit complete hook
	observability.Pipeline().OnOrderingComplete(context.Background(), o.crossings, time.Since(o.startTime))

	o.cli.Logger.Debug("ordering result", "crossings", o.crossings)

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

	if p.renderStats.Crossings == 0 {
		ui.PrintSuccess("Render complete (optimal layout)")
	} else {
		ui.PrintInfo("Render complete (%d crossings remaining)", p.renderStats.Crossings)
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
func deriveBasePath(input, output string) string {
	if output != "" {
		ext := filepath.Ext(output)
		if pipeline.ValidFormats[strings.TrimPrefix(ext, ".")] {
			return strings.TrimSuffix(output, ext)
		}
		return output
	}
	base := strings.TrimSuffix(input, filepath.Ext(input))
	return strings.TrimSuffix(base, ".layout")
}

// writeFile writes raw data to the specified path (or stdout if empty).
func writeFile(data []byte, path string) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// countCrossingsFromLayout computes edge crossings from the layout's
// row orderings and edges. It builds a temporary DAG for counting.
func countCrossingsFromLayout(layout graph.Layout) int {
	if len(layout.Rows) == 0 || len(layout.Edges) == 0 {
		return 0
	}

	// Build a DAG from layout data
	g := dag.New(nil)

	// Add all nodes from rows with their row assignments
	for row, nodeIDs := range layout.Rows {
		for _, id := range nodeIDs {
			_ = g.AddNode(dag.Node{ID: id, Row: row})
		}
	}

	// Add edges
	for _, e := range layout.Edges {
		_ = g.AddEdge(dag.Edge{From: e.From, To: e.To})
	}

	return dag.CountCrossings(g, layout.Rows)
}

// formatCount formats a number with K/M suffixes for readability.
func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
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
