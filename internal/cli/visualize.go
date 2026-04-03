package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/pipeline"
)

// visualizeCommand creates the visualize command for rendering from a layout.
func (c *CLI) visualizeCommand() *cobra.Command {
	var (
		formatsStr string
		output     string
		noCache    bool
	)
	opts := pipeline.Options{}
	setCLIDefaults(&opts)

	cmd := &cobra.Command{
		Use:   "visualize [layout.json]",
		Short: "Render visualization from a computed layout",
		Long: `Render visualization from a computed layout.

The visualize command takes a layout.json file (produced by 'layout') and
renders it to SVG, PNG, or PDF format. The layout contains all positioning
information, so this step is purely about rendering.

Results are cached locally for faster subsequent runs.

Use 'render' as a shortcut to go directly from graph.json to visual output.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Formats = parseFormats(formatsStr)
			if err := pipeline.ValidateFormats(opts.Formats); err != nil {
				return err
			}
			if err := pipeline.ValidateStyle(opts.Style); err != nil {
				return err
			}
			return c.runVisualize(cmd.Context(), args[0], opts, output, noCache)
		},
	}

	// Common flags
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (single format) or base path (multiple)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "disable caching")

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

// runVisualize loads the layout and renders it.
func (c *CLI) runVisualize(ctx context.Context, input string, opts pipeline.Options, output string, noCache bool) error {
	start := time.Now()

	layout, err := graph.ReadLayoutFile(input)
	if err != nil {
		return WrapSystemError(err, fmt.Sprintf("failed to load layout %s", input), "Check that the file exists and is valid JSON.")
	}

	// Infer viz type from layout
	vizType := layout.VizType
	if vizType == "" {
		vizType = graph.VizTypeTower
	}
	opts.VizType = vizType

	runner, err := c.newRunner(noCache, false)
	if err != nil {
		return WrapSystemError(err, "failed to initialize runner", "This may be a cache or configuration issue.")
	}
	defer runner.Close()

	opts.Logger = c.Logger
	if opts.Style == "" && layout.Style != "" {
		opts.Style = layout.Style
	}

	spinner := ui.NewSpinnerWithContext(ctx, fmt.Sprintf("Rendering %s...", vizType))
	spinner.Start()

	artifacts, cacheHit, err := runner.RenderWithCacheInfo(ctx, layout, nil, opts)
	if err != nil {
		spinner.StopWithError("Visualization failed")
		return WrapSystemError(err, "visualization failed", "Check the output format and try again.")
	}
	spinner.Stop()

	return writeArtifacts(artifactWriteParams{
		artifacts: artifacts,
		formats:   opts.Formats,
		input:     input,
		output:    output,
		nodeCount: len(layout.Nodes),
		edgeCount: len(layout.Edges),
		cacheHit:  cacheHit,
		elapsed:   time.Since(start),
	})
}
