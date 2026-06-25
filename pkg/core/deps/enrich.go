package deps

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/observability"
)

var enrichAuthHintOnce sync.Once

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

	// Build a node-ID to registry package name mapping. When node IDs are
	// versioned (e.g. "lodash@4.17.21"), registries expect the bare name.
	// Use meta["name"] when present, otherwise use the node ID directly.
	nodeToName := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.ID == ProjectRootNodeID {
			continue
		}
		if name, ok := n.Meta["name"].(string); ok && name != "" {
			nodeToName[n.ID] = name
		} else {
			nodeToName[n.ID] = n.ID
		}
	}
	if len(nodeToName) == 0 {
		return stats
	}

	// Collect registry package names for URL fetching (deduplicated).
	nameSet := make(map[string]bool, len(nodeToName))
	for _, name := range nodeToName {
		nameSet[name] = true
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
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

	// Build PackageRef list with URLs populated from URLProvider.
	// Use the registry package name for lookups, but keep the node ID for
	// mapping results back to graph nodes.
	type refEntry struct {
		ref    *PackageRef
		nodeID string
	}
	entries := make([]refEntry, 0, len(nodeToName))
	for _, n := range nodes {
		if n.ID == ProjectRootNodeID {
			continue
		}
		pkgName := nodeToName[n.ID]
		version, _ := n.Meta["version"].(string)
		ref := &PackageRef{
			Name:         pkgName,
			Version:      version,
			ManifestFile: manifestFile,
		}
		if urlMap != nil {
			if urls, ok := urlMap[pkgName]; ok {
				ref.ProjectURLs = urls.ProjectURLs
				ref.HomePage = urls.HomePage
			}
		}
		entries = append(entries, refEntry{ref: ref, nodeID: n.ID})
	}
	refs := make([]*PackageRef, len(entries))
	for i, e := range entries {
		refs[i] = e.ref
	}
	stats.Total = len(refs)

	// Try batch enrichment first (e.g. GitHub GraphQL — one call for all).
	// This emits OnEnrichStart/OnEnrichComplete hooks for progress UI.
	// All batch providers run and their results are merged; the per-package
	// fallback below is used only when no batch provider succeeded.
	hooks := observability.ResolverFromContext(ctx)
	enrichedNodes := make(map[string]bool)
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
		providerHits := 0
		for _, e := range entries {
			// Batch results are keyed by PackageRef.Name (registry name).
			if extra, ok := batch[e.ref.Name]; ok {
				if n, found := g.Node(e.nodeID); found {
					maps.Copy(n.Meta, extra)
					enrichedNodes[e.nodeID] = true
					providerHits++
				}
			}
		}
		hooks.OnEnrichComplete(ctx, p.Name(), providerHits, nil)
	}
	if stats.UsedBatch {
		stats.Succeeded = len(enrichedNodes)
		stats.Failed = stats.Total - stats.Succeeded
		return stats
	}

	// Fallback: parallel per-package enrichment using ParallelMapOrdered.
	type enrichJob struct {
		ref    *PackageRef
		nodeID string
	}
	jobs := make([]enrichJob, 0, len(entries))
	for _, e := range entries {
		jobs = append(jobs, enrichJob(e))
	}

	type enrichResult struct {
		nodeID  string
		meta    map[string]any
		success bool
	}

	var authErrorSeen bool
	var authMu sync.Mutex

	results, _ := ParallelMapOrdered(ctx, o.Workers, jobs, func(ctx context.Context, j enrichJob) enrichResult {
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
		return enrichResult{nodeID: j.nodeID, meta: m, success: success}
	})

	for _, res := range results {
		if n, ok := g.Node(res.nodeID); ok {
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
