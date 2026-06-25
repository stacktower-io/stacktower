package github

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stacktower-io/stacktower/pkg/integrations"
)

// maxReposPerQuery is the maximum repos in a single GraphQL request.
// GitHub GraphQL has a node limit; 100 repos is safe and well within it.
const maxReposPerQuery = 100

// RepoID identifies a GitHub repository for batch fetching.
type RepoID struct {
	Owner string
	Name  string
}

func (r RepoID) Key() string { return r.Owner + "/" + r.Name }

// graphqlRequest is the JSON body sent to GitHub's GraphQL endpoint.
type graphqlRequest struct {
	Query string `json:"query"`
}

// graphqlResponse is the top-level GraphQL response.
// Repo values are pointers: GitHub returns "rN": null for repositories that
// don't exist or aren't accessible, and a nil entry must be distinguishable
// from a present repo with zero values (0 stars, empty fields).
type graphqlResponse struct {
	Data   map[string]*graphqlRepo `json:"data"`
	Errors []graphqlError          `json:"errors"`
}

type graphqlError struct {
	Message string `json:"message"`
}

type graphqlRepo struct {
	StargazerCount int    `json:"stargazerCount"`
	Description    string `json:"description"`
	IsArchived     bool   `json:"isArchived"`
	PushedAt       string `json:"pushedAt"`
	LicenseInfo    *struct {
		SpdxID string `json:"spdxId"`
	} `json:"licenseInfo"`
	PrimaryLanguage *struct {
		Name string `json:"name"`
	} `json:"primaryLanguage"`
	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string `json:"name"`
			} `json:"topic"`
		} `json:"nodes"`
	} `json:"repositoryTopics"`
	LatestRelease *struct {
		PublishedAt string `json:"publishedAt"`
	} `json:"latestRelease"`
}

// repoFragment is the GraphQL fields fetched for each repository.
const repoFragment = `
    stargazerCount
    description
    isArchived
    pushedAt
    licenseInfo { spdxId }
    primaryLanguage { name }
    repositoryTopics(first: 10) { nodes { topic { name } } }
    latestRelease { publishedAt }
`

// buildGraphQLQuery constructs a batched query for multiple repositories.
func buildGraphQLQuery(repos []RepoID) string {
	var b strings.Builder
	b.WriteString("query {")
	for i, r := range repos {
		fmt.Fprintf(&b, "\n  r%d: repository(owner: %q, name: %q) {%s}", i, r.Owner, r.Name, repoFragment)
	}
	b.WriteString("\n}")
	return b.String()
}

// FetchBatch retrieves repository metrics for multiple repos in a single GraphQL call.
// Repos that don't exist or fail are silently omitted from the result, as are
// repos with invalid owner/name (they are never sent to the API).
// Returns a map keyed by "owner/repo" to RepoMetrics.
//
// If refresh is true, the cache is bypassed for this batch. GraphQL-level
// errors (partial failures, rate limits) are checked and returned.
func (c *Client) FetchBatch(ctx context.Context, repos []RepoID, refresh bool) (map[string]*integrations.RepoMetrics, error) {
	// Owner/name often come from untrusted package metadata; validate before
	// they are interpolated into the GraphQL query (mirrors Fetch). Invalid
	// repos are skipped rather than failing the whole batch.
	valid := make([]RepoID, 0, len(repos))
	for _, r := range repos {
		if err := ValidateRepoRef(r.Owner, r.Name); err != nil {
			continue
		}
		valid = append(valid, r)
	}
	repos = valid

	result := make(map[string]*integrations.RepoMetrics, len(repos))

	// Process in batches of maxReposPerQuery
	for start := 0; start < len(repos); start += maxReposPerQuery {
		end := start + maxReposPerQuery
		if end > len(repos) {
			end = len(repos)
		}
		batch := repos[start:end]

		metrics, err := c.fetchGraphQLBatch(ctx, batch, refresh)
		if err != nil {
			return nil, err
		}
		for k, v := range metrics {
			result[k] = v
		}
	}

	return result, nil
}

func (c *Client) fetchGraphQLBatch(ctx context.Context, repos []RepoID, refresh bool) (map[string]*integrations.RepoMetrics, error) {
	// Sort the batch so that identical repo sets produce the same query,
	// cache key, and alias ("rN") positions regardless of input order.
	// Without this, the same set in a different order creates duplicate
	// cache entries (and cached aliases would map to the wrong repos).
	sorted := make([]RepoID, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key() < sorted[j].Key() })
	repos = sorted

	query := buildGraphQLQuery(repos)
	url := strings.TrimSuffix(c.baseURL, "/")
	graphqlURL := strings.Replace(url, "https://api.github.com", "https://api.github.com/graphql", 1)
	if graphqlURL == url {
		graphqlURL = url + "/graphql"
	}

	keyParts := make([]string, 0, len(repos))
	for _, r := range repos {
		keyParts = append(keyParts, r.Key())
	}
	cacheKey := "graphql_batch:" + strings.Join(keyParts, ",")

	var resp graphqlResponse
	err := c.Cached(ctx, cacheKey, refresh, &resp, func() error {
		return c.PostJSON(ctx, graphqlURL, graphqlRequest{Query: query}, &resp)
	})
	if err != nil {
		return nil, fmt.Errorf("graphql batch fetch: %w", err)
	}

	// Check for GraphQL-level errors (rate limits, schema issues, etc.)
	if len(resp.Errors) > 0 {
		var msgs []string
		for _, e := range resp.Errors {
			msgs = append(msgs, e.Message)
		}
		joined := strings.Join(msgs, "; ")
		// Detect rate limit errors so the circuit breaker and retry logic can react.
		for _, e := range resp.Errors {
			lower := strings.ToLower(e.Message)
			if strings.Contains(lower, "rate limit") || strings.Contains(lower, "api rate") {
				return nil, &integrations.RateLimitedError{RetryAfter: 60}
			}
		}
		return nil, fmt.Errorf("graphql errors: %s", joined)
	}

	result := make(map[string]*integrations.RepoMetrics, len(repos))
	for i, repo := range repos {
		alias := fmt.Sprintf("r%d", i)
		data, ok := resp.Data[alias]
		if !ok || data == nil {
			// Missing alias or "rN": null — the repo doesn't exist or isn't
			// accessible; omit it instead of reporting zero-valued metrics.
			continue
		}
		result[repo.Key()] = graphqlRepoToMetrics(repo, *data)
	}

	return result, nil
}

func graphqlRepoToMetrics(repo RepoID, data graphqlRepo) *integrations.RepoMetrics {
	m := &integrations.RepoMetrics{
		RepoURL:  fmt.Sprintf("https://github.com/%s/%s", repo.Owner, repo.Name),
		Owner:    repo.Owner,
		Stars:    data.StargazerCount,
		Archived: data.IsArchived,
	}
	if data.Description != "" {
		m.Description = data.Description
	}
	if data.LicenseInfo != nil {
		m.License = normalizeLicense(data.LicenseInfo.SpdxID)
	}
	if data.PrimaryLanguage != nil {
		m.Language = data.PrimaryLanguage.Name
	}
	if data.PushedAt != "" {
		if t, err := time.Parse(time.RFC3339, data.PushedAt); err == nil {
			m.LastCommitAt = &t
		}
	}
	if data.LatestRelease != nil && data.LatestRelease.PublishedAt != "" {
		if t, err := time.Parse(time.RFC3339, data.LatestRelease.PublishedAt); err == nil {
			m.LastReleaseAt = &t
		}
	}
	var topics []string
	for _, node := range data.RepositoryTopics.Nodes {
		if node.Topic.Name != "" {
			topics = append(topics, node.Topic.Name)
		}
	}
	if len(topics) > 0 {
		m.Topics = topics
	}
	return m
}

// FetchContributorsBatch fetches contributors for multiple repos concurrently.
// This uses the REST API (not GraphQL) since contributors aren't available via GraphQL.
// Repos that fail are silently skipped. Returns a map keyed by "owner/repo".
func (c *Client) FetchContributorsBatch(ctx context.Context, repos []RepoID) map[string][]integrations.Contributor {
	result := make(map[string][]integrations.Contributor)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrency to avoid overwhelming the API
	sem := make(chan struct{}, 10)

	for _, repo := range repos {
		wg.Add(1)
		go func(r RepoID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			contribs, err := c.fetchContributors(ctx, r.Owner, r.Name)
			if err != nil {
				return // silently skip failures
			}

			mu.Lock()
			result[r.Key()] = contribs
			mu.Unlock()
		}(repo)
	}

	wg.Wait()
	return result
}
