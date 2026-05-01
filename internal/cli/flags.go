package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

// Convention used throughout this file:
//
//	cmd.Flags().BoolVar(&opts.X, "x", opts.X, ...)
//
// `opts.X` appears twice because the caller must have already populated
// the options struct via setCLIDefaults (i.e. the "cli" preset). The
// second `opts.X` captures the preset's value as the flag default, so the
// preset is the single source of truth for what's on/off by default.
//
// Help text uses boolHelp() to surface "(default: off)" for preset-off
// booleans (cobra already shows "(default true)" for preset-on ones), so
// `--help` readers don't have to trace the preset chain to know what
// happens without the flag. Booleans default to their preset value;
// passing `--x=false` disables a preset-on flag.

// addLayoutFlags registers the flags that control layout computation.
// These are shared by `layout` and `render` (which is a layout+visualize
// shortcut). `orderTimeout` is passed by pointer so the caller can read the
// parsed value after Cobra invokes RunE.
func addLayoutFlags(cmd *cobra.Command, opts *pipeline.Options, orderTimeout *int) {
	cmd.Flags().StringVarP(&opts.VizType, "type", "t", opts.VizType, "visualization type: tower, nodelink")
	cmd.Flags().BoolVar(&opts.Normalize, "normalize", opts.Normalize, boolHelp("apply graph normalization", opts.Normalize))
	cmd.Flags().Float64Var(&opts.Width, "width", opts.Width, "frame width")
	cmd.Flags().Float64Var(&opts.Height, "height", opts.Height, "frame height")
	cmd.Flags().StringVar(&opts.Ordering, "ordering", opts.Ordering, "ordering algorithm: optimal, barycentric")
	cmd.Flags().BoolVar(&opts.Randomize, "randomize", opts.Randomize, boolHelp("randomize block widths (tower)", opts.Randomize))
	cmd.Flags().BoolVar(&opts.Merge, "merge", opts.Merge, boolHelp("merge subdivider blocks (tower)", opts.Merge))
	cmd.Flags().BoolVar(&opts.Nebraska, "nebraska", opts.Nebraska, boolHelp("show Nebraska maintainer ranking (tower)", opts.Nebraska))
	cmd.Flags().IntVar(orderTimeout, "ordering-timeout", defaultOrderTimeout, "timeout in seconds for optimal ordering search")
}

// addRenderFlags registers the flags that control visual rendering (style,
// edges, hover behaviour, output format). Shared by `render` and
// `visualize`. `formatsStr` is a pointer so the caller can parse it after
// Cobra populates it.
func addRenderFlags(cmd *cobra.Command, opts *pipeline.Options, formatsStr *string) {
	cmd.Flags().StringVar(&opts.Style, "style", opts.Style, "visual style: handdrawn, simple")
	cmd.Flags().BoolVar(&opts.ShowEdges, "edges", opts.ShowEdges, boolHelp("show dependency edges (tower)", opts.ShowEdges))
	cmd.Flags().BoolVar(&opts.Popups, "popups", opts.Popups, boolHelp("show hover popups with metadata", opts.Popups))
	cmd.Flags().StringVarP(formatsStr, "format", "f", "", "output format(s): svg, json, pdf, png (comma-separated)")
}

// addSecurityFlags registers flags that toggle vulnerability/license
// overlays. Shared by `layout`, `render`, and `visualize`.
func addSecurityFlags(cmd *cobra.Command, opts *pipeline.Options) {
	cmd.Flags().BoolVar(&opts.ShowVulns, "show-vulns", opts.ShowVulns, boolHelp("show vulnerability severity colours (requires scanned graph)", opts.ShowVulns))
	cmd.Flags().BoolVar(&opts.ShowLicenses, "show-licenses", opts.ShowLicenses, boolHelp("show license compliance indicators (copyleft/unknown borders)", opts.ShowLicenses))
	cmd.Flags().BoolVar(&opts.FlagsOnTop, "flags-on-top", opts.FlagsOnTop, boolHelp("render security flags on top of all blocks", opts.FlagsOnTop))
}

// boolHelp appends "(default: on)" or "(default: off)" to a flag
// description so users reading --help can see the preset behaviour
// without tracing setCLIDefaults.
// Cobra automatically appends "(default true)" when a bool flag defaults
// to true but stays silent for false defaults (since false is the zero
// value). We only decorate the false case so users reading --help can
// still see the preset behaviour without duplicating cobra's own
// annotation.
func boolHelp(desc string, enabled bool) string {
	if enabled {
		return desc
	}
	return desc + " (default: off)"
}

func validateRenderFormats(formats []string) error {
	if err := pipeline.ValidateFormats(formats); err != nil {
		return NewUserError(err.Error(), "Supported formats: svg, json, pdf, png")
	}
	return nil
}

func validateRenderStyle(style string) error {
	if err := pipeline.ValidateStyle(style); err != nil {
		return NewUserError(err.Error(), "Supported styles: handdrawn, simple")
	}
	return nil
}

func validateOrderingTimeout(seconds int) error {
	if seconds < 0 {
		return NewUserError(
			fmt.Sprintf("invalid ordering-timeout: %d", seconds),
			"ordering-timeout must be 0 or greater",
		)
	}
	return nil
}
