package metadata

import (
	"context"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/integrations"
	"github.com/matzehuels/stacktower/pkg/integrations/github"
)

// Compile-time check that GitHub implements BatchMetadataProvider.
var _ deps.BatchMetadataProvider = (*GitHub)(nil)

type GitHub struct {
	client            *github.Client
	fetchContributors bool
}

// GitHubOption configures the GitHub metadata provider.
type GitHubOption func(*GitHub)

// WithContributors enables fetching contributor data from GitHub.
// This requires additional API calls per repository and is slower,
// but enables accurate Nebraska (maintainer) rankings.
func WithContributors() GitHubOption {
	return func(g *GitHub) { g.fetchContributors = true }
}

func NewGitHub(backend cache.Cache, token string, cacheTTL time.Duration, opts ...GitHubOption) *GitHub {
	c := github.NewClient(backend, token, cacheTTL)
	g := &GitHub{client: c}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

func (g *GitHub) Name() string { return "github" }

func (g *GitHub) Enrich(ctx context.Context, pkg *deps.PackageRef, refresh bool) (map[string]any, error) {
	owner, name, ok := github.ExtractURL(pkg.ProjectURLs, pkg.HomePage)
	if !ok {
		return nil, nil
	}

	m, err := g.client.Fetch(ctx, owner, name, refresh)
	if err != nil {
		return nil, err
	}

	return metricsToMap(m), nil
}

// EnrichBatch fetches metadata for all packages in one or two GraphQL calls.
// Only packages with GitHub URLs in their registry metadata (ProjectURLs, HomePage)
// are enriched. Packages without discoverable URLs are silently skipped - we don't
// use SearchPackageRepo here because it's too slow/rate-limited for batch operations.
// If WithContributors() was used, additional REST API calls fetch contributor data.
func (g *GitHub) EnrichBatch(ctx context.Context, pkgs []*deps.PackageRef, refresh bool) (map[string]map[string]any, error) {
	// Phase 1: resolve each package to a GitHub owner/repo
	type resolved struct {
		pkg  *deps.PackageRef
		repo github.RepoID
	}
	var repoList []github.RepoID
	var resolvedPkgs []resolved
	seen := make(map[string]bool)

	for _, pkg := range pkgs {
		owner, name, ok := github.ExtractURL(pkg.ProjectURLs, pkg.HomePage)
		if !ok {
			// Skip packages without GitHub URLs in their registry metadata.
			// SearchPackageRepo is too slow/rate-limited for batch enrichment.
			continue
		}
		repo := github.RepoID{Owner: owner, Name: name}
		resolvedPkgs = append(resolvedPkgs, resolved{pkg: pkg, repo: repo})
		if !seen[repo.Key()] {
			seen[repo.Key()] = true
			repoList = append(repoList, repo)
		}
	}

	if len(repoList) == 0 {
		return nil, nil
	}

	// Phase 2: single batched GraphQL fetch for all repos
	metrics, err := g.client.FetchBatch(ctx, repoList, refresh)
	if err != nil {
		return nil, err
	}

	// Phase 2.5: optionally fetch contributors via REST API
	var contributors map[string][]integrations.Contributor
	if g.fetchContributors {
		contributors = g.client.FetchContributorsBatch(ctx, repoList)
		// Merge contributors into metrics
		for key, contribs := range contributors {
			if m, ok := metrics[key]; ok {
				m.Contributors = contribs
			}
		}
	}

	// Phase 3: map results back to package names
	result := make(map[string]map[string]any, len(resolvedPkgs))
	for _, rp := range resolvedPkgs {
		m, ok := metrics[rp.repo.Key()]
		if !ok {
			continue
		}
		result[rp.pkg.Name] = metricsToMap(m)
	}

	return result, nil
}

// metricsToMap converts RepoMetrics to the metadata map format used by node.Meta.
func metricsToMap(m *integrations.RepoMetrics) map[string]any {
	result := map[string]any{
		RepoURL:      m.RepoURL,
		RepoOwner:    m.Owner,
		RepoStars:    m.Stars,
		RepoArchived: m.Archived,
	}
	if m.Description != "" {
		result[RepoDescription] = m.Description
	}
	if m.Language != "" {
		result[RepoLanguage] = m.Language
	}
	if m.License != "" {
		result[RepoLicense] = m.License
		result["license"] = m.License
	}
	if len(m.Topics) > 0 {
		result[RepoTopics] = m.Topics
	}
	if m.LastCommitAt != nil {
		result[RepoLastCommit] = m.LastCommitAt.Format("2006-01-02")
	}
	if m.LastReleaseAt != nil {
		result[RepoLastRelease] = m.LastReleaseAt.Format("2006-01-02")
	}
	if len(m.Contributors) > 0 {
		maintainers := make([]string, len(m.Contributors))
		for i, c := range m.Contributors {
			maintainers[i] = c.Login
		}
		result[RepoMaintainers] = maintainers
	}
	return result
}
