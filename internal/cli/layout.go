package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
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
			if err := validateRenderStyle(opts.Style); err != nil {
				return err
			}
			if err := validateOrderingTimeout(orderTimeout); err != nil {
				return err
			}
			return c.runLayout(cmd.Context(), args[0], opts, output, noCache, orderTimeout)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default: <input>.layout.json)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable caching")
	addLayoutFlags(cmd, &opts, &orderTimeout)
	// 'style' is shared with visualize but at the layout stage it only
	// affects style defaults baked into the layout; it stays here to
	// preserve existing CLI UX (users can already pass --style to layout).
	cmd.Flags().StringVar(&opts.Style, "style", opts.Style, "visual style: handdrawn (default), simple")
	addSecurityFlags(cmd, &opts)

	return cmd
}

// runLayout loads the graph, computes the layout, and writes output.
func (c *CLI) runLayout(ctx context.Context, input string, opts pipeline.Options, output string, noCache bool, orderTimeout int) error {
	start := time.Now()

	g, err := loadGraph(input)
	if err != nil {
		hint := "Check that the file exists and is valid JSON."
		if input == "-" {
			hint = "Pipe valid graph JSON to stdin."
		}
		return WrapSystemError(err, fmt.Sprintf("failed to load graph %s", input), hint)
	}

	runner, err := c.newRunner(noCache, false)
	if err != nil {
		return WrapSystemError(err, "failed to initialize runner", "This may be a cache or configuration issue.")
	}
	defer runner.Close()

	opts.Logger = c.Logger
	if opts.NeedsOptimalOrderer() {
		opts.Orderer = c.newOptimalOrderer(ctx, orderTimeout)
	}

	workGraph, err := runner.PrepareGraph(g, opts)
	if err != nil {
		return WrapSystemError(err, "graph normalization failed", "The dependency graph may contain invalid structure.")
	}

	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Computing %s layout...", opts.VizType))
	if opts.NeedsOptimalOrderer() {
		c.AttachOrderingSpinner(spinner)
		defer c.AttachOrderingSpinner(nil)
	}
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
