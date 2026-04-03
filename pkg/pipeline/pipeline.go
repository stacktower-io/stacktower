// Package pipeline provides the core visualization pipeline for Stacktower.
//
// This package implements the complete parse → layout → render pipeline that
// can be used by CLI, API, and worker components. By centralizing this logic,
// we ensure consistent behavior across all entry points and avoid code duplication.
//
// # Architecture
//
// The pipeline consists of three stages:
//
//  1. Parse: Resolve dependencies from package registries or manifest files
//  2. Layout: Compute visual positions for the dependency graph
//  3. Render: Generate output in various formats (SVG, PNG, PDF, JSON)
//
// Each stage can be run independently or as part of the complete pipeline.
//
// # Usage
//
// Create a Runner and execute the pipeline:
//
//	runner := pipeline.NewRunner(cache, nil, logger)
//	opts := pipeline.Options{
//	    Language: "python",
//	    Package:  "requests",
//	    VizType:  "tower",
//	    Formats:  []string{"svg"},
//	}
//	result, err := runner.Execute(ctx, opts)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	svg := result.Artifacts["svg"]
//
// Run individual stages:
//
//	// Parse only
//	g, err := runner.Parse(ctx, parseOpts)
//
//	// Layout with existing graph
//	layout, err := runner.ComputeLayout(ctx, g, layoutOpts)
//
//	// Render with existing layout
//	artifacts, err := runner.Render(ctx, layout, g, renderOpts)
package pipeline

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/log"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/ordering"
	"github.com/matzehuels/stacktower/pkg/graph"
)

// =============================================================================
// Default Values - Single Source of Truth for CLI, API, and Worker
// =============================================================================

const (
	// DefaultMaxDepth is the maximum dependency traversal depth for the pipeline.
	// This is intentionally more conservative than deps.DefaultMaxDepth (50) to
	// provide better UX for CLI users and prevent excessively large graphs.
	// API users can override this by setting MaxDepth explicitly.
	DefaultMaxDepth = 10

	// DefaultMaxNodes is the maximum number of nodes to fetch.
	// This matches deps.DefaultMaxNodes (5000) to maintain consistency.
	DefaultMaxNodes = 5000

	// DefaultWidth is the default frame width in pixels.
	DefaultWidth = 800.0

	// DefaultHeight is the default frame height in pixels.
	DefaultHeight = 600.0

	// DefaultSeed is the default random seed for reproducibility.
	DefaultSeed = uint64(42)

	// DefaultOrdering is the default ordering algorithm.
	DefaultOrdering = "optimal"
)

// DefaultVizType is the default visualization type.
const DefaultVizType = graph.VizTypeTower

// DefaultStyle is the default visual style.
const DefaultStyle = graph.StyleHanddrawn

// Format constants for output formats.
const (
	FormatSVG  = "svg"
	FormatPNG  = "png"
	FormatPDF  = "pdf"
	FormatJSON = "json"
)

// ValidFormats is the set of supported output formats.
var ValidFormats = map[string]bool{
	FormatSVG:  true,
	FormatPNG:  true,
	FormatPDF:  true,
	FormatJSON: true,
}

// ValidStyles is the set of supported visual styles.
var ValidStyles = map[string]bool{
	graph.StyleSimple:    true,
	graph.StyleHanddrawn: true,
}

// ValidVizTypes is the set of supported visualization types.
var ValidVizTypes = map[string]bool{
	graph.VizTypeTower:    true,
	graph.VizTypeNodelink: true,
}

// =============================================================================
// Options - Pipeline Configuration
// =============================================================================

// Options contains all configuration for the visualization pipeline.
// This struct supports JSON serialization for API requests.
type Options struct {
	// Parse options
	Language          string `json:"language"`
	Package           string `json:"package,omitempty"`
	Version           string `json:"version,omitempty"` // Specific package version (e.g., "2.31.0")
	Manifest          string `json:"manifest,omitempty"`
	ManifestFilename  string `json:"manifest_filename,omitempty"`
	ManifestPath      string `json:"manifest_path,omitempty"` // Optional on-disk path used when parser needs workspace context
	Owner             string `json:"owner,omitempty"`         // GitHub owner (user/org)
	Repo              string `json:"repo,omitempty"`          // GitHub repository name
	Ref               string `json:"ref,omitempty"`           // Git ref (branch/tag)
	Path              string `json:"path,omitempty"`          // Path within repo
	RootName          string `json:"root_name,omitempty"`     // Custom name for root node (replaces __project__)
	MaxDepth          int    `json:"max_depth,omitempty"`
	MaxNodes          int    `json:"max_nodes,omitempty"`
	Workers           int    `json:"workers,omitempty"`            // Concurrent fetch workers (0 = default 20)
	SkipEnrich        bool   `json:"skip_enrich,omitempty"`        // Skip metadata enrichment (default: false = enrich)
	FetchContributors bool   `json:"fetch_contributors,omitempty"` // Fetch GitHub contributors (slower, enables Nebraska rankings)
	Refresh           bool   `json:"refresh,omitempty"`
	DependencyScope   string `json:"dependency_scope,omitempty"`   // Dependency scope policy: prod_only (default) or all
	IncludePrerelease bool   `json:"include_prerelease,omitempty"` // Include prerelease versions (alpha/beta/rc/dev/etc.)
	RuntimeVersion    string `json:"runtime_version,omitempty"`    // Target runtime version for marker evaluation (e.g., "3.11" for Python)

	// Layout options
	VizType   string  `json:"viz_type,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
	Normalize bool    `json:"normalize,omitempty"` // Apply graph normalization during layout
	Ordering  string  `json:"ordering,omitempty"`
	Merge     bool    `json:"merge,omitempty"`
	Randomize bool    `json:"randomize,omitempty"`
	Seed      uint64  `json:"seed,omitempty"`

	// Render options
	Formats    []string `json:"formats,omitempty"`
	Style      string   `json:"style,omitempty"`
	ShowEdges  bool     `json:"show_edges,omitempty"`
	Nebraska   bool     `json:"nebraska,omitempty"` // Show Nebraska ranking panel in SVG (data is always computed)
	Popups     bool     `json:"popups,omitempty"`
	FlagsOnTop bool     `json:"flags_on_top,omitempty"` // Render security flags (license/vuln) on top of all blocks

	// Security options
	SecurityScan bool `json:"security_scan,omitempty"` // Run vulnerability scan during parse
	ShowVulns    bool `json:"show_vulns,omitempty"`    // Include vulnerability data in rendered output
	ShowLicenses bool `json:"show_licenses,omitempty"` // Analyze and show license compliance data in rendered output

	// Runtime options (not serialized)
	Logger      *log.Logger      `json:"-"`
	GitHubToken string           `json:"-"`
	Orderer     ordering.Orderer `json:"-"`

	// validated tracks whether ValidateAndSetDefaults has been called.
	validated bool `json:"-"`
}

// Result contains the outputs of a pipeline run.
type Result struct {
	// Graph is the parsed dependency graph.
	Graph *dag.DAG

	// GraphHash is the content hash of the graph.
	GraphHash string

	// Layout contains the layout data (positions, nebraska, etc).
	Layout graph.Layout

	// Artifacts contains rendered outputs keyed by format.
	Artifacts map[string][]byte

	// Stats contains timing and size information.
	Stats Stats

	// CacheInfo tracks which stages hit the cache.
	CacheInfo CacheInfo

	// RuntimeVersion is the target runtime version used for resolution.
	// For Python: "3.11", for Node.js: "20", etc.
	RuntimeVersion string

	// RuntimeSource indicates where the runtime version came from.
	// Values: "manifest" (detected from file), "cli" (user specified), "default" (language default)
	RuntimeSource string
}

// Stats contains pipeline execution statistics.
type Stats struct {
	NodeCount  int
	EdgeCount  int
	ParseTime  time.Duration
	LayoutTime time.Duration
	RenderTime time.Duration
}

// CacheInfo tracks cache hits for each pipeline stage.
type CacheInfo struct {
	ParseHit  bool // Whether parse result came from cache
	LayoutHit bool // Whether layout result came from cache
	RenderHit bool // Whether all artifacts came from cache
}

// =============================================================================
// Validation Functions
// =============================================================================

// ValidateFormat checks that a format is valid.
func ValidateFormat(format string) error {
	if !ValidFormats[format] {
		return fmt.Errorf("invalid format: %q (must be one of: svg, png, pdf, json)", format)
	}
	return nil
}

// ValidateFormats checks that all formats are valid.
func ValidateFormats(formats []string) error {
	for _, f := range formats {
		if err := ValidateFormat(f); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStyle checks that a style is valid.
func ValidateStyle(style string) error {
	if !ValidStyles[style] {
		return fmt.Errorf("invalid style: %q (must be one of: simple, handdrawn)", style)
	}
	return nil
}

// ValidateVizType checks that a visualization type is valid.
func ValidateVizType(vizType string) error {
	if !ValidVizTypes[vizType] {
		return fmt.Errorf("invalid viz_type: %q (must be one of: tower, nodelink)", vizType)
	}
	return nil
}

// =============================================================================
// Options Methods
// =============================================================================

// ValidateAndSetDefaults checks required fields and applies defaults for the full pipeline.
// This method is idempotent - calling it multiple times has the same effect as calling it once.
func (o *Options) ValidateAndSetDefaults() error {
	if o.validated {
		return nil
	}
	if err := o.ValidateForParse(); err != nil {
		return err
	}
	o.SetLayoutDefaults()
	o.SetRenderDefaults()
	o.validated = true
	return nil
}

// ValidateForParse checks required fields for parsing.
func (o *Options) ValidateForParse() error {
	if o.Language == "" {
		return fmt.Errorf("language is required")
	}
	if o.Package == "" && o.Manifest == "" {
		return fmt.Errorf("package or manifest is required")
	}
	if o.Manifest != "" && o.ManifestFilename == "" {
		return fmt.Errorf("manifest_filename is required")
	}

	// Parse defaults
	if o.MaxDepth == 0 {
		o.MaxDepth = DefaultMaxDepth
	}
	if o.MaxNodes == 0 {
		o.MaxNodes = DefaultMaxNodes
	}
	if o.DependencyScope == "" {
		o.DependencyScope = deps.DependencyScopeProdOnly
	}
	if o.DependencyScope != deps.DependencyScopeProdOnly && o.DependencyScope != deps.DependencyScopeAll {
		return fmt.Errorf("invalid dependency_scope: %q (must be one of: %s, %s)", o.DependencyScope, deps.DependencyScopeProdOnly, deps.DependencyScopeAll)
	}

	// Logger default
	if o.Logger == nil {
		o.Logger = log.NewWithOptions(io.Discard, log.Options{})
	}

	return nil
}

// SetLayoutDefaults sets default values for layout computation.
func (o *Options) SetLayoutDefaults() {
	if o.VizType == "" {
		o.VizType = DefaultVizType
	}
	if o.Width == 0 {
		o.Width = DefaultWidth
	}
	if o.Height == 0 {
		o.Height = DefaultHeight
	}
	if o.Seed == 0 {
		o.Seed = DefaultSeed
	}
	if o.Logger == nil {
		o.Logger = log.NewWithOptions(io.Discard, log.Options{})
	}
}

// ValidateForLayout validates and sets defaults for layout computation.
func (o *Options) ValidateForLayout() error {
	o.SetLayoutDefaults()
	return ValidateVizType(o.VizType)
}

// SetRenderDefaults sets default values for rendering.
func (o *Options) SetRenderDefaults() {
	if len(o.Formats) == 0 {
		o.Formats = []string{FormatSVG}
	}
	if o.Style == "" {
		o.Style = DefaultStyle
	}
	if o.Logger == nil {
		o.Logger = log.NewWithOptions(io.Discard, log.Options{})
	}
}

// =============================================================================
// Presets - Consumer-Specific Defaults
// =============================================================================

// Preset names for ApplyPreset.
const (
	// PresetCLI applies defaults optimized for interactive CLI usage.
	// Enables: Randomize, Merge, Normalize, Popups, ShowVulns, ShowLicenses.
	PresetCLI = "cli"

	// PresetAPI applies defaults optimized for API/programmatic usage.
	// Uses minimal defaults suitable for embedding in web applications.
	PresetAPI = "api"

	// PresetWorker applies defaults optimized for background worker processing.
	// Similar to API but may include additional options for batch processing.
	PresetWorker = "worker"
)

// ApplyPreset applies consumer-specific defaults on top of base pipeline defaults.
// This should be called after setting Language/Package but before validation.
//
// Presets:
//   - "cli": Interactive CLI with rich visualizations (randomize, merge, popups, vulns, licenses)
//   - "api": Minimal defaults for programmatic/web usage
//   - "worker": Background processing defaults (similar to API)
//
// Unknown presets are silently ignored (base defaults apply).
func (o *Options) ApplyPreset(preset string) {
	o.SetLayoutDefaults()
	o.SetRenderDefaults()

	switch preset {
	case PresetCLI:
		o.Randomize = true
		o.Merge = true
		o.Normalize = true
		o.Popups = true
		o.ShowVulns = true
		o.ShowLicenses = true
		o.FlagsOnTop = true
	case PresetAPI:
		o.Randomize = false
		o.Merge = false
		o.Normalize = false
		o.Popups = false
		o.ShowVulns = false
		o.ShowLicenses = false
		o.FlagsOnTop = true
	case PresetWorker:
		o.Randomize = false
		o.Merge = false
		o.Normalize = false
		o.Popups = false
		o.ShowVulns = false
		o.ShowLicenses = false
		o.FlagsOnTop = true
	}
}

// ValidateForRender validates and sets defaults for rendering.
func (o *Options) ValidateForRender() error {
	o.SetLayoutDefaults()
	o.SetRenderDefaults()
	if err := ValidateVizType(o.VizType); err != nil {
		return err
	}
	if err := ValidateFormats(o.Formats); err != nil {
		return err
	}
	return ValidateStyle(o.Style)
}

// IsTower returns true if this is a tower visualization.
func (o *Options) IsTower() bool {
	return o.VizType == "" || o.VizType == graph.VizTypeTower
}

// IsNodelink returns true if this is a nodelink visualization.
func (o *Options) IsNodelink() bool {
	return o.VizType == graph.VizTypeNodelink
}

// NeedsOptimalOrderer returns true if the ordering algorithm requires the optimal orderer.
// This is true when ordering is "optimal" (the default) or empty.
func (o *Options) NeedsOptimalOrderer() bool {
	return o.Ordering == DefaultOrdering || o.Ordering == ""
}

// ShouldEnrich returns whether metadata enrichment should be performed.
func (o *Options) ShouldEnrich() bool {
	return !o.SkipEnrich
}

// LayoutKeyOpts returns cache key options for layout computation.
func (o *Options) LayoutKeyOpts() cache.LayoutKeyOpts {
	return cache.LayoutKeyOpts{
		VizType:   o.VizType,
		Width:     o.Width,
		Height:    o.Height,
		Normalize: o.Normalize,
		Ordering:  o.Ordering,
		Merge:     o.Merge,
		Randomize: o.Randomize,
		Seed:      o.Seed,
	}
}

// ArtifactKeyOpts returns cache key options for artifact rendering.
func (o *Options) ArtifactKeyOpts(format string) cache.ArtifactKeyOpts {
	return cache.ArtifactKeyOpts{
		Format:       format,
		Style:        o.Style,
		ShowEdges:    o.ShowEdges,
		Popups:       o.Popups,
		Nebraska:     o.Nebraska,
		Merge:        o.Merge,
		Normalize:    o.Normalize,
		ShowVulns:    o.ShowVulns,
		ShowLicenses: o.ShowLicenses,
		FlagsOnTop:   o.FlagsOnTop,
	}
}
