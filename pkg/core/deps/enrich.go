package deps

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/observability"
)

var enrichAuthHintOnce sync.Once

// graphEnrichJob represents a single package to enrich in EnrichGraph.
type graphEnrichJob struct {
	ref *PackageRef
}

// graphEnrichResult holds the enrichment result for a package in EnrichGraph.
type graphEnrichResult struct {
	name    string
	meta    map[string]any
	success bool
}

// EnrichStats contains statistics about the enrichment process.
// This is useful for observability and debugging.
type EnrichStats struct {
	// Total is the number of packages that were candidates for enrichment.
	Total int
	// Succeeded is the number of packages that were successfully enriched
	// (metadata was added to the node).
	Succeeded int
	// Failed is the number of packages where enrichment failed.
	Failed int
	// AuthError indicates if an authentication error was encountered.
	// This typically means a GitHub token is missing, expired, or invalid.
	AuthError bool
	// UsedBatch indicates if batch enrichment was used (vs per-package fallback).
	UsedBatch bool
}

// EnrichGraph adds external metadata (e.g. GitHub stars) to every non-root graph
// node. It prefers batch providers (one API call for all packages) and falls
// back to parallel per-package enrichment using ParallelMapOrdered.
//
// The manifestFile parameter specifies the manifest file name for PackageRef
// (e.g., "package.json", "Cargo.toml", "pyproject.toml").
//
// If opts.URLProvider is set, it fetches package URLs from the registry before
// enrichment. This enables GitHub enrichment for manifest files that don't
// include repository URLs directly (e.g., lock files).
//
// Returns EnrichStats with counts of successful/failed enrichments for observability.
// This is the standard enrichment pattern for all lock file parsers.
func EnrichGraph(ctx context.Context, g *dag.DAG, manifestFile string, opts Options) EnrichStats {
	stats := EnrichStats{}
	if len(opts.MetadataProviders) == 0 {
		return stats
	}

	o := opts.WithDefaults()
	if ctx == nil {
		ctx = o.Ctx
	}

	nodes := g.Nodes()

	// Collect package names (excluding project root)
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ID != ProjectRootNodeID {
			names = append(names, n.ID)
		}
	}
	if len(names) == 0 {
		return stats
	}

	// Fetch URLs from registry if URLProvider is available.
	// This enables enrichment for manifest files that don't include repository URLs.
	var urlMap map[string]PackageURLs
	if opts.URLProvider != nil {
		var err error
		urlMap, err = opts.URLProvider.FetchURLs(ctx, names, opts.Refresh)
		if err != nil {
			opts.Logger("url fetch failed: %v", err)
		}
	}

	// Build PackageRef list with URLs populated from URLProvider
	refs := make([]*PackageRef, 0, len(names))
	for _, n := range nodes {
		if n.ID == ProjectRootNodeID {
			continue
		}
		version, _ := n.Meta["version"].(string)
		ref := &PackageRef{
			Name:         n.ID,
			Version:      version,
			ManifestFile: manifestFile,
		}
		// Populate URLs if available from registry lookup
		if urlMap != nil {
			if urls, ok := urlMap[n.ID]; ok {
				ref.ProjectURLs = urls.ProjectURLs
				ref.HomePage = urls.HomePage
			}
		}
		refs = append(refs, ref)
	}
	stats.Total = len(refs)

	// Try batch enrichment first (e.g. GitHub GraphQL — one call for all).
	// This emits OnEnrichStart/OnEnrichComplete hooks for progress UI.
	hooks := observability.ResolverFromContext(ctx)
	for _, p := range opts.MetadataProviders {
		bp, ok := p.(BatchMetadataProvider)
		if !ok {
			continue
		}
		hooks.OnEnrichStart(ctx, p.Name(), len(refs))
		batch, err := bp.EnrichBatch(ctx, refs, opts.Refresh)
		if err != nil {
			hooks.OnEnrichComplete(ctx, p.Name(), 0, err)
			opts.Logger("batch enrich (%s): %v", p.Name(), err)
			if errors.Is(err, cache.ErrUnauthorized) {
				stats.AuthError = true
			}
			continue
		}
		stats.UsedBatch = true
		for _, n := range nodes {
			if extra, ok := batch[n.ID]; ok {
				maps.Copy(n.Meta, extra)
				stats.Succeeded++
			}
		}
		stats.Failed = stats.Total - stats.Succeeded
		hooks.OnEnrichComplete(ctx, p.Name(), stats.Succeeded, nil)
		return stats // batch provider handled everything
	}

	// Fallback: parallel per-package enrichment using ParallelMapOrdered.
	jobs := make([]graphEnrichJob, 0, len(refs))
	for _, ref := range refs {
		jobs = append(jobs, graphEnrichJob{ref: ref})
	}

	var authErrorSeen bool
	var authMu sync.Mutex

	results := ParallelMapOrdered(ctx, o.Workers, jobs, func(ctx context.Context, j graphEnrichJob) graphEnrichResult {
		hooks.OnFetchStart(ctx, j.ref.Name, 0)
		m := make(map[string]any)
		success := false
		for _, p := range opts.MetadataProviders {
			enriched, err := p.Enrich(ctx, j.ref, opts.Refresh)
			if err != nil {
				opts.Logger("enrich failed: %s: %v", j.ref.Name, err)
				if errors.Is(err, cache.ErrUnauthorized) {
					authMu.Lock()
					authErrorSeen = true
					authMu.Unlock()
					enrichAuthHintOnce.Do(func() {
						opts.Logger("hint: GitHub token may be expired. Run 'stacktower github logout && stacktower github login' to re-authenticate")
					})
				}
				continue
			}
			maps.Copy(m, enriched)
			success = true
		}
		hooks.OnFetchComplete(ctx, j.ref.Name, 0, 0, nil)
		return graphEnrichResult{name: j.ref.Name, meta: m, success: success}
	})

	for _, res := range results {
		if n, ok := g.Node(res.name); ok {
			maps.Copy(n.Meta, res.meta)
		}
		if res.success {
			stats.Succeeded++
		} else {
			stats.Failed++
		}
	}
	stats.AuthError = authErrorSeen
	return stats
}
