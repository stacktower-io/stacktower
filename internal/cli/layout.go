package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/pipeline"
)

// layoutCommand creates the layout command for computing visualization layouts.
func (c *CLI) layoutCommand() *cobra.Command {
	var (
		output       string
		noCache      bool
		orderTimeout int
	)
	opts := pipeline.Options{}
	setCLIDefaults(&opts)

	cmd := &cobra.Command{
		Use:   "layout [graph.json]",
		Short: "Compute visualization layout from a dependency graph",
		Long: `Compute visualization layout from a dependency graph.

The layout command takes a graph.json file (produced by 'parse') and computes
the layout for visualization. The output is a layout.json file (same format as
'render -f json') that can be rendered to SVG/PNG/PDF using the 'visualize' command.

Supports both tower (-t tower) and nodelink (-t nodelink) visualization types.

Results are cached locally for faster subsequent runs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runLayout(cmd.Context(), args[0], opts, output, noCache, orderTimeout)
		},
	}

	// Common flags
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default: <input>.layout.json)")
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
	cmd.Flags().StringVar(&opts.Style, "style", opts.Style, "visual style: handdrawn (default), simple")

	// Security flags
	cmd.Flags().BoolVar(&opts.ShowVulns, "show-vulns", opts.ShowVulns, "show vulnerability severity colours (requires scanned graph)")
	cmd.Flags().BoolVar(&opts.ShowLicenses, "show-licenses", opts.ShowLicenses, "show license compliance indicators (copyleft/unknown borders)")
	cmd.Flags().BoolVar(&opts.FlagsOnTop, "flags-on-top", opts.FlagsOnTop, "render security flags on top of all blocks")

	return cmd
}

// runLayout loads the graph, computes the layout, and writes output.
func (c *CLI) runLayout(ctx context.Context, input string, opts pipeline.Options, output string, noCache bool, orderTimeout int) error {
	start := time.Now()

	g, err := graph.ReadGraphFile(input)
	if err != nil {
		return WrapSystemError(err, fmt.Sprintf("failed to load graph %s", input), "Check that the file exists and is valid JSON.")
	}

	runner, err := c.newRunner(noCache, false)
	if err != nil {
		return WrapSystemError(err, "failed to initialize runner", "This may be a cache or configuration issue.")
	}
	defer runner.Close()

	opts.Logger = c.Logger
	if opts.NeedsOptimalOrderer() {
		opts.Orderer = c.newOptimalOrderer(orderTimeout)
	}

	workGraph, err := runner.PrepareGraph(g, opts)
	if err != nil {
		return WrapSystemError(err, "graph normalization failed", "The dependency graph may contain invalid structure.")
	}

	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Computing %s layout...", opts.VizType))
	spinner.Start()

	layout, cacheHit, err := runner.GenerateLayoutWithCacheInfo(ctx, workGraph, opts)
	if err != nil {
		spinner.StopWithError("Layout failed")
		return WrapSystemError(err, "layout computation failed", "Try reducing max-nodes or simplifying the graph.")
	}
	spinner.Stop()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	outputPath := output
	if outputPath == "" {
		base := strings.TrimSuffix(input, filepath.Ext(input))
		outputPath = base + ".layout.json"
	}

	if err := graph.WriteLayoutFile(layout, outputPath); err != nil {
		return WrapSystemError(err, fmt.Sprintf("failed to write output %s", outputPath), "Check that the output path is writable.")
	}

	ui.PrintSuccess("Layout complete")
	ui.PrintFile(outputPath)
	ui.PrintStats(g.NodeCount(), g.EdgeCount(), 0, cacheHit, time.Since(start))
	ui.PrintNewline()
	ui.PrintNextStep("Render", "stacktower visualize "+outputPath)

	return nil
}
