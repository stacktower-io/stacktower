package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	dagtransform "github.com/matzehuels/stacktower/pkg/core/dag/transform"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/ordering"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/observability"
	"github.com/matzehuels/stacktower/pkg/security"
)

// Runner encapsulates pipeline execution with caching.
// Both CLI and API can use this to avoid duplicating caching logic.
//
// The Runner is stateless except for the cache, logger, and scanner — it
// doesn't store pipeline results. Multiple goroutines can safely use the
// same Runner with different options.
type Runner struct {
	Cache   cache.Cache
	Keyer   cache.Keyer
	Logger  *log.Logger
	Scanner security.Scanner // Optional vulnerability scanner (nil = scanning disabled)

	// Hooks provides optional per-runner observability hooks.
	// When set, these override the global hooks from the observability package.
	// This enables multi-tenant scenarios where different API requests need
	// different observability backends (e.g., per-request tracing).
	Hooks *RunnerHooks
}

// RunnerHooks contains optional observability hooks for a Runner.
// Any nil field falls back to the global hook from the observability package.
type RunnerHooks struct {
	Pipeline observability.PipelineHooks
	Security observability.SecurityHooks
}

// NewRunner creates a runner with the given cache and keyer.
// If keyer is nil, a DefaultKeyer is used.
// If cache is nil, a NullCache is used (caching disabled).
func NewRunner(c cache.Cache, keyer cache.Keyer, logger *log.Logger) *Runner {
	return NewRunnerWithScanner(c, keyer, logger, nil)
}

// NewRunnerWithScanner creates a runner with an optional security scanner.
// If scanner is nil, security scanning is unavailable even when opts.SecurityScan is true.
func NewRunnerWithScanner(c cache.Cache, keyer cache.Keyer, logger *log.Logger, scanner security.Scanner) *Runner {
	if keyer == nil {
		keyer = cache.NewDefaultKeyer()
	}
	if c == nil {
		c = cache.NewNullCache()
	}
	if logger == nil {
		logger = log.Default()
	}
	return &Runner{
		Cache:   c,
		Keyer:   keyer,
		Logger:  logger,
		Scanner: scanner,
	}
}

// Execute runs the complete parse → layout → render pipeline with caching.
func (r *Runner) Execute(ctx context.Context, opts Options) (*Result, error) {
	if err := opts.ValidateAndSetDefaults(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
	r.applyLogger(&opts)

	result := &Result{
		Artifacts: make(map[string][]byte),
	}

	// Stage 1: Parse
	parseStart := time.Now()
	parseResult, err := r.ParseWithCacheInfo(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	result.Graph = parseResult.Graph
	result.Stats.ParseTime = time.Since(parseStart)
	result.Stats.NodeCount = parseResult.Graph.NodeCount()
	result.Stats.EdgeCount = parseResult.Graph.EdgeCount()
	result.CacheInfo.ParseHit = parseResult.CacheHit
	result.RuntimeVersion = parseResult.RuntimeVersion
	result.RuntimeSource = parseResult.RuntimeSource

	// Compute graph hash for cache keys and API responses
	if graphData, err := graph.MarshalGraph(parseResult.Graph); err == nil {
		result.GraphHash = cache.Hash(graphData)
	}

	r.Logger.Debug("parsed dependencies",
		"nodes", parseResult.Graph.NodeCount(),
		"edges", parseResult.Graph.EdgeCount(),
		"duration", result.Stats.ParseTime)

	// Apply normalization if requested (controlled by opts.Normalize)
	workGraph, err := r.PrepareGraph(parseResult.Graph, opts)
	if err != nil {
		return nil, err
	}

	// Stage 2: Layout
	layoutStart := time.Now()
	layout, layoutHit, err := r.GenerateLayoutWithCacheInfo(ctx, workGraph, opts)
	if err != nil {
		return nil, fmt.Errorf("layout: %w", err)
	}
	result.Layout = layout
	result.Stats.LayoutTime = time.Since(layoutStart)
	result.CacheInfo.LayoutHit = layoutHit

	r.Logger.Debug("computed layout",
		"blocks", len(layout.Blocks),
		"duration", result.Stats.LayoutTime)

	// Stage 3: Render
	renderStart := time.Now()
	artifacts, renderHit, err := r.RenderWithCacheInfo(ctx, layout, workGraph, opts)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	result.Artifacts = artifacts
	result.Stats.RenderTime = time.Since(renderStart)
	result.CacheInfo.RenderHit = renderHit

	r.Logger.Debug("rendered outputs",
		"formats", opts.Formats,
		"duration", result.Stats.RenderTime)

	return result, nil
}

// ParseResultWithCacheInfo contains the parsed dependency graph plus metadata.
type ParseResultWithCacheInfo struct {
	Graph          *dag.DAG
	CacheHit       bool
	RuntimeVersion string // Target runtime version used (e.g., "3.11")
	RuntimeSource  string // Where runtime came from: "cli", "manifest", "default"
}

func (r *Runner) ParseWithCacheInfo(ctx context.Context, opts Options) (*ParseResultWithCacheInfo, error) {
	if err := opts.ValidateForParse(); err != nil {
		return nil, err
	}
	r.applyLogger(&opts)

	pkg := opts.Package
	if opts.Version != "" {
		pkg = opts.Package + "@" + opts.Version
	}

	r.pipelineHooksCtx(ctx).OnParseStart(ctx, opts.Language, pkg)
	start := time.Now()

	result, err := r.parseWithCache(ctx, opts)

	nodeCountVal := 0
	if result != nil {
		nodeCountVal = nodeCount(result.Graph)
	}
	r.pipelineHooksCtx(ctx).OnParseComplete(ctx, opts.Language, pkg, nodeCountVal, time.Since(start), err)
	return result, err
}

func (r *Runner) parseWithCache(ctx context.Context, opts Options) (*ParseResultWithCacheInfo, error) {
	pkgOrManifest := opts.Package
	if opts.Version != "" {
		pkgOrManifest = opts.Package + "@" + opts.Version
	}
	if opts.Manifest != "" {
		pkgOrManifest = cache.Hash([]byte(opts.Manifest))
	}
	enriched := opts.ShouldEnrich()
	cacheKey := r.Keyer.GraphKey(opts.Language, pkgOrManifest, cache.GraphKeyOpts{
		MaxDepth:          opts.MaxDepth,
		MaxNodes:          opts.MaxNodes,
		Enriched:          enriched,
		SecurityScan:      opts.SecurityScan,
		IncludePrerelease: opts.IncludePrerelease,
		DependencyScope:   opts.DependencyScope,
		RuntimeVersion:    opts.RuntimeVersion,
	})

	if !opts.Refresh {
		if data, hit, err := r.Cache.Get(ctx, cacheKey); err == nil && hit {
			g, err := graph.ReadGraph(bytes.NewReader(data))
			if err == nil {
				// Cache hit: read runtime info from graph metadata (stored during parse)
				runtimeVersion, _ := g.Meta()["runtime_version"].(string)
				runtimeSource, _ := g.Meta()["runtime_source"].(string)
				// Fallback if metadata is missing (older cache entries)
				if runtimeVersion == "" {
					if opts.RuntimeVersion != "" {
						runtimeVersion = opts.RuntimeVersion
						runtimeSource = "cli"
					} else if lang := languages.Find(opts.Language); lang != nil {
						runtimeVersion = lang.DefaultRuntimeVersion
						runtimeSource = "default"
					}
				}
				return &ParseResultWithCacheInfo{
					Graph:          g,
					CacheHit:       true,
					RuntimeVersion: runtimeVersion,
					RuntimeSource:  runtimeSource,
				}, nil
			}
		}
	}

	parseResult, err := Parse(ctx, r.Cache, opts)
	if err != nil {
		return nil, err
	}

	if opts.SecurityScan && r.Scanner != nil {
		if scanErr := r.runSecurityScan(ctx, parseResult.Graph, opts.Language); scanErr != nil {
			r.Logger.Warn("security scan failed", "err", scanErr)
		}
	}

	if !opts.Refresh {
		if data, err := graph.MarshalGraph(parseResult.Graph); err == nil {
			r.setCacheWithWarning(ctx, cacheKey, data, cache.TTLGraph, "parse")
		}
	}

	return &ParseResultWithCacheInfo{
		Graph:          parseResult.Graph,
		CacheHit:       false,
		RuntimeVersion: parseResult.RuntimeVersion,
		RuntimeSource:  parseResult.RuntimeSource,
	}, nil
}

// runSecurityScan scans dependencies for known vulnerabilities and annotates the graph.
func (r *Runner) runSecurityScan(ctx context.Context, g *dag.DAG, language string) error {
	deps := security.DependenciesFromDAG(g, language)
	if len(deps) == 0 {
		return nil
	}

	r.Logger.Debug("scanning dependencies for vulnerabilities", "deps", len(deps))

	report, err := r.Scanner.Scan(ctx, deps)
	if err != nil {
		return fmt.Errorf("vulnerability scan: %w", err)
	}

	security.AnnotateGraph(g, report)

	if len(report.Findings) > 0 {
		r.Logger.Warn("vulnerabilities found",
			"total", len(report.Findings),
			"vulnerable_deps", report.VulnerableDeps,
			"max_severity", report.MaxSeverity())
	} else {
		r.Logger.Debug("no known vulnerabilities found")
	}

	return nil
}

// Parse is a convenience wrapper that calls ParseWithCacheInfo and discards the cache hit info.
func (r *Runner) Parse(ctx context.Context, opts Options) (*dag.DAG, error) {
	result, err := r.ParseWithCacheInfo(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.Graph, nil
}

// GenerateLayoutWithCacheInfo generates a layout with caching and returns cache hit info.
func (r *Runner) GenerateLayoutWithCacheInfo(ctx context.Context, g *dag.DAG, opts Options) (graph.Layout, bool, error) {
	if err := opts.ValidateForLayout(); err != nil {
		return graph.Layout{}, false, err
	}
	r.applyLogger(&opts)

	r.pipelineHooksCtx(ctx).OnLayoutStart(ctx, opts.VizType, g.NodeCount())
	start := time.Now()

	graphData, _ := graph.MarshalGraph(g)
	graphHash := cache.Hash(graphData)
	cacheKey := r.Keyer.LayoutKey(graphHash, opts.LayoutKeyOpts())

	if data, hit, err := r.Cache.Get(ctx, cacheKey); err == nil && hit {
		cached, err := graph.UnmarshalLayout(data)
		if err == nil {
			r.pipelineHooksCtx(ctx).OnLayoutComplete(ctx, opts.VizType, time.Since(start), nil)
			return cached, true, nil
		}
	}

	layout, err := GenerateLayout(g, opts)
	r.pipelineHooksCtx(ctx).OnLayoutComplete(ctx, opts.VizType, time.Since(start), err)
	if err != nil {
		return graph.Layout{}, false, err
	}

	if data, err := graph.MarshalLayout(layout); err == nil {
		r.setCacheWithWarning(ctx, cacheKey, data, cache.TTLLayout, "layout")
	}

	return layout, false, nil
}

// GenerateLayout is a convenience wrapper that calls GenerateLayoutWithCacheInfo and discards the cache hit info.
func (r *Runner) GenerateLayout(ctx context.Context, g *dag.DAG, opts Options) (graph.Layout, error) {
	layout, _, err := r.GenerateLayoutWithCacheInfo(ctx, g, opts)
	return layout, err
}

// RenderWithCacheInfo generates artifacts with caching and returns cache hit info.
func (r *Runner) RenderWithCacheInfo(ctx context.Context, layout graph.Layout, g *dag.DAG, opts Options) (map[string][]byte, bool, error) {
	if err := opts.ValidateForRender(); err != nil {
		return nil, false, err
	}
	r.applyLogger(&opts)

	r.pipelineHooksCtx(ctx).OnRenderStart(ctx, opts.Formats)
	start := time.Now()

	layoutData, err := graph.MarshalLayout(layout)
	if err != nil {
		renderErr := fmt.Errorf("serialize layout for cache key: %w", err)
		r.pipelineHooksCtx(ctx).OnRenderComplete(ctx, opts.Formats, time.Since(start), renderErr)
		return nil, false, renderErr
	}
	cacheKeyHash := cache.Hash(layoutData)

	allCached := true
	artifacts := make(map[string][]byte)

	for _, format := range opts.Formats {
		cacheKey := r.Keyer.ArtifactKey(cacheKeyHash, opts.ArtifactKeyOpts(format))
		if data, hit, err := r.Cache.Get(ctx, cacheKey); err == nil && hit {
			artifacts[format] = data
		} else {
			allCached = false
			break
		}
	}

	if allCached && len(artifacts) == len(opts.Formats) {
		r.pipelineHooksCtx(ctx).OnRenderComplete(ctx, opts.Formats, time.Since(start), nil)
		return artifacts, true, nil
	}

	rendered, err := RenderFromLayout(layout, g, opts)
	r.pipelineHooksCtx(ctx).OnRenderComplete(ctx, opts.Formats, time.Since(start), err)
	if err != nil {
		return nil, false, err
	}

	for format, data := range rendered {
		cacheKey := r.Keyer.ArtifactKey(cacheKeyHash, opts.ArtifactKeyOpts(format))
		r.setCacheWithWarning(ctx, cacheKey, data, cache.TTLArtifact, "render")
	}

	return rendered, false, nil
}

// Render is a convenience wrapper that calls RenderWithCacheInfo and discards the cache hit info.
func (r *Runner) Render(ctx context.Context, layout graph.Layout, g *dag.DAG, opts Options) (map[string][]byte, error) {
	artifacts, _, err := r.RenderWithCacheInfo(ctx, layout, g, opts)
	return artifacts, err
}

// PrepareGraph applies normalization and optionally strips vulnerability/license data.
// Returns the original graph if no transformations are needed.
//
// When opts.ShowVulns is false, vulnerability metadata is removed from nodes
// so that downstream renderers do not colour nodes by severity.
// When opts.ShowLicenses is true, license risk analysis is run and annotated.
// When opts.ShowLicenses is false, any existing license risk metadata is stripped.
func (r *Runner) PrepareGraph(g *dag.DAG, opts Options) (*dag.DAG, error) {
	normalize := opts.Normalize && !opts.IsNodelink()
	needsClone := normalize || !opts.ShowVulns || opts.ShowLicenses

	if !needsClone {
		return g, nil
	}

	workGraph := g.Clone()

	if !opts.ShowVulns {
		security.StripVulnData(workGraph)
	}

	// License compliance analysis
	if opts.ShowLicenses {
		report := security.AnalyzeLicenses(workGraph)
		if !report.Compliant {
			r.Logger.Warn("license compliance issues",
				"copyleft", len(report.Copyleft),
				"weak_copyleft", len(report.WeakCopyleft),
				"unknown", len(report.Unknown))
		} else {
			r.Logger.Debug("all licenses compliant",
				"total", report.TotalDeps,
				"license_types", len(report.Licenses))
		}
	} else {
		security.StripLicenseData(workGraph)
	}

	if normalize {
		if _, err := dagtransform.Normalize(workGraph); err != nil {
			return nil, fmt.Errorf("normalize graph: %w", err)
		}
		r.Logger.Debug("normalized graph",
			"original_nodes", g.NodeCount(),
			"normalized_nodes", workGraph.NodeCount())
	}

	return workGraph, nil
}

// Close releases resources held by the runner (primarily the cache).
func (r *Runner) Close() error {
	if r.Cache != nil {
		return r.Cache.Close()
	}
	return nil
}

// applyLogger sets the runner's logger on options if not already set.
func (r *Runner) applyLogger(opts *Options) {
	if opts.Logger == nil {
		opts.Logger = r.Logger
	}
}

func (r *Runner) setCacheWithWarning(ctx context.Context, key string, data []byte, ttl time.Duration, stage string) {
	if err := r.Cache.Set(ctx, key, data, ttl); err != nil {
		r.Logger.Warn("cache write failed", "stage", stage, "key", key, "err", err)
	}
}

func nodeCount(g *dag.DAG) int {
	if g == nil {
		return 0
	}
	return g.NodeCount()
}

// pipelineHooksCtx returns pipeline hooks with context support.
// Priority: context hooks > runner hooks > global hooks.
func (r *Runner) pipelineHooksCtx(ctx context.Context) observability.PipelineHooks {
	if hooks := observability.PipelineFromContext(ctx); hooks != nil {
		if _, isNoop := hooks.(observability.NoopPipelineHooks); !isNoop {
			return hooks
		}
	}
	if r.Hooks != nil && r.Hooks.Pipeline != nil {
		return r.Hooks.Pipeline
	}
	return observability.Pipeline()
}

// NewOptimalOrderer creates an optimal search orderer with progress reporting
// wired to the runner's pipeline hooks. This is a convenience for consumers
// who want an orderer with progress callbacks that integrate with observability.
//
// Example:
//
//	orderer := runner.NewOptimalOrderer(60 * time.Second)
//	opts.Orderer = orderer
//	result, err := runner.Execute(ctx, opts)
func (r *Runner) NewOptimalOrderer(timeout time.Duration) *OrdererWithHooks {
	return &OrdererWithHooks{
		Timeout: timeout,
		hooks:   r.pipelineHooksCtx(context.Background()),
	}
}

// OrdererWithHooks wraps ordering.OptimalSearch with hooks-based progress reporting.
type OrdererWithHooks struct {
	Timeout   time.Duration
	hooks     observability.PipelineHooks
	startTime time.Time
	rowCount  int
}

// OrderRows implements ordering.Orderer.
func (o *OrdererWithHooks) OrderRows(g *dag.DAG) map[int][]string {
	o.startTime = time.Now()
	o.rowCount = g.RowCount()

	o.hooks.OnOrderingStart(context.Background(), "optimal", o.rowCount)

	search := ordering.OptimalSearch{
		Timeout:  o.Timeout,
		Progress: o.onProgress,
	}

	result := search.OrderRows(g)
	crossings := dag.CountCrossings(g, result)

	o.hooks.OnOrderingComplete(context.Background(), crossings, time.Since(o.startTime))

	return result
}

func (o *OrdererWithHooks) onProgress(explored, pruned, bestScore int) {
	o.hooks.OnOrderingProgress(context.Background(), explored, pruned, bestScore)
}
