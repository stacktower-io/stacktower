package pipeline

import (
	"fmt"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	corerender "github.com/matzehuels/stacktower/pkg/core/render"
	"github.com/matzehuels/stacktower/pkg/core/render/nodelink"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/layout"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/sink"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/styles"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/styles/handdrawn"
	"github.com/matzehuels/stacktower/pkg/graph"
)

// Render generates output artifacts in the requested formats.
func Render(l layout.Layout, g *dag.DAG, opts Options) (map[string][]byte, error) {
	if opts.IsNodelink() {
		// For nodelink, generate DOT graph on-demand
		return renderNodelinkFromGraph(g, opts)
	}
	return renderTower(l, g, opts)
}

// RenderNodelink generates nodelink outputs from a layout.
// The layout must be a nodelink layout (VizType = "nodelink") with a DOT string.
func RenderNodelink(layout graph.Layout, opts Options) (map[string][]byte, error) {
	if layout.DOT == "" {
		return nil, fmt.Errorf("nodelink layout missing DOT string")
	}

	artifacts := make(map[string][]byte)
	needsSVG := false
	for _, format := range opts.Formats {
		if format == FormatSVG || format == FormatPNG || format == FormatPDF {
			needsSVG = true
			break
		}
	}

	var svgData []byte
	if needsSVG {
		var err error
		svgData, err = nodelink.RenderSVG(layout.DOT)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", FormatSVG, err)
		}
	}

	for _, format := range opts.Formats {
		var data []byte
		var err error

		switch format {
		case FormatSVG:
			data = svgData
		case FormatPNG:
			data, err = corerender.ToPNG(svgData, 2.0)
		case FormatPDF:
			data, err = corerender.ToPDF(svgData)
		case FormatJSON:
			data, err = graph.MarshalLayout(layout)
		default:
			return nil, fmt.Errorf("unsupported nodelink format: %s", format)
		}

		if err != nil {
			return nil, fmt.Errorf("render %s: %w", format, err)
		}
		artifacts[format] = data
	}

	return artifacts, nil
}

// renderNodelinkFromGraph generates nodelink outputs directly from a graph.
// This generates the DOT graph on-demand instead of requiring a pre-computed layout.
func renderNodelinkFromGraph(g *dag.DAG, opts Options) (map[string][]byte, error) {
	// Generate DOT graph
	dot := nodelink.ToDOT(g, nodelink.Options{Detailed: false})

	// Build layout
	layout, err := nodelink.Export(dot, g, nodelink.Options{Detailed: false}, opts.Width, opts.Height, opts.Style)
	if err != nil {
		return nil, fmt.Errorf("generate nodelink layout: %w", err)
	}

	// Render using the layout
	return RenderNodelink(layout, opts)
}

// renderTower generates tower outputs.
func renderTower(l layout.Layout, g *dag.DAG, opts Options) (map[string][]byte, error) {
	opts = applyLayoutMetadata(opts, l)

	svgOpts := buildSVGOptions(g, l, opts)
	artifacts := make(map[string][]byte)
	needsSVG := false
	for _, format := range opts.Formats {
		if format == FormatSVG || format == FormatPNG || format == FormatPDF {
			needsSVG = true
			break
		}
	}
	var svgData []byte
	if needsSVG {
		svgData = sink.RenderSVG(l, svgOpts...)
	}

	for _, format := range opts.Formats {
		var data []byte
		var err error

		switch format {
		case FormatSVG:
			data = svgData
		case FormatPNG:
			data, err = corerender.ToPNG(svgData, 2.0)
		case FormatPDF:
			data, err = corerender.ToPDF(svgData)
		case FormatJSON:
			var exported graph.Layout
			exported, err = l.Export(g)
			if err != nil {
				return nil, fmt.Errorf("serialize layout: %w", err)
			}
			data, err = graph.MarshalLayout(exported)
		default:
			return nil, fmt.Errorf("unsupported tower format: %s", format)
		}

		if err != nil {
			return nil, fmt.Errorf("render %s: %w", format, err)
		}
		artifacts[format] = data
	}

	return artifacts, nil
}

// applyLayoutMetadata applies layout metadata to options if not already set.
// This ensures that serialized layouts preserve their original rendering settings.
func applyLayoutMetadata(opts Options, l layout.Layout) Options {
	if opts.Style == "" && l.Style != "" {
		opts.Style = l.Style
	}
	if opts.Seed == 0 && l.Seed != 0 {
		opts.Seed = l.Seed
	}
	if !opts.Merge && l.Merged {
		opts.Merge = l.Merged
	}
	return opts
}

// buildSVGOptions builds SVG rendering options.
// Nebraska rankings are extracted from the layout struct (computed during layout phase).
func buildSVGOptions(g *dag.DAG, l layout.Layout, opts Options) []sink.SVGOption {
	var svgOpts []sink.SVGOption

	if g != nil {
		svgOpts = append(svgOpts, sink.WithGraph(g))
	}
	if opts.ShowEdges {
		svgOpts = append(svgOpts, sink.WithEdges())
	}
	if opts.Merge {
		svgOpts = append(svgOpts, sink.WithMerged())
	}

	// Apply visual style
	switch opts.Style {
	case graph.StyleHanddrawn:
		seed := opts.Seed
		if seed == 0 {
			seed = 42
		}
		svgOpts = append(svgOpts, sink.WithStyle(handdrawn.New(seed)))
	case graph.StyleSimple:
		svgOpts = append(svgOpts, sink.WithStyle(styles.Simple{}))
	}

	// Popups only for handdrawn style (simple doesn't support them yet)
	if opts.Style == graph.StyleHanddrawn && opts.Popups && g != nil {
		svgOpts = append(svgOpts, sink.WithPopups())
	}

	// Nebraska guy ranking panel - only rendered if opts.Nebraska is true.
	// Note: The Nebraska data is ALWAYS computed during layout generation;
	// this flag only controls whether the panel is displayed in the SVG.
	if opts.Nebraska && len(l.Nebraska) > 0 {
		svgOpts = append(svgOpts, sink.WithNebraska(l.Nebraska))
	}

	// Security flags rendering position
	svgOpts = append(svgOpts, sink.WithFlagsOnTop(opts.FlagsOnTop))

	return svgOpts
}

// RenderFromLayoutData renders output from serialized layout data.
// This is useful when the layout was computed elsewhere (e.g., cached).
func RenderFromLayoutData(layoutData []byte, g *dag.DAG, opts Options) (map[string][]byte, error) {
	// Parse layout
	parsed, err := graph.UnmarshalLayout(layoutData)
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}

	// Dispatch based on viz type
	return RenderFromLayout(parsed, g, opts)
}

// RenderFromLayout renders output from a graph.Layout.
// This is the preferred entry point when you have a graph.Layout.
func RenderFromLayout(graphLayout graph.Layout, g *dag.DAG, opts Options) (map[string][]byte, error) {
	if graphLayout.IsNodelink() {
		opts.VizType = graph.VizTypeNodelink
		return RenderNodelink(graphLayout, opts)
	}

	// Convert to internal tower layout
	l, err := layout.Parse(graphLayout)
	if err != nil {
		return nil, fmt.Errorf("convert layout: %w", err)
	}

	// renderTower applies layout metadata via applyLayoutMetadata
	return renderTower(l, g, opts)
}
