package github

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

var repoURLPattern = regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:[/?#]|$)`)

// Client provides access to the GitHub API for repository metadata enrichment.
// It handles HTTP requests with caching, automatic retries, and optional authentication.
//
// All methods are safe for concurrent use by multiple goroutines.
type Client struct {
	*integrations.Client
	baseURL string
}

// NewClient creates a GitHub API client with optional authentication and proactive rate limiting.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - token: GitHub personal access token (empty string for unauthenticated)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// Rate limits are configured via integrations.DefaultRateLimits:
//   - Unauthenticated ("github_unauth"): 60 requests/hour per IP (~0.016 req/s)
//   - Authenticated ("github"): 5,000 requests/hour per token (~1.4 req/s)
//
// Authentication is strongly recommended for production use to avoid rate limiting.
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, token string, cacheTTL time.Duration) *Client {
	headers := map[string]string{"Accept": "application/vnd.github.v3+json"}

	var rl integrations.RateLimit
	if token != "" {
		headers["Authorization"] = "Bearer " + token
		rl = integrations.DefaultRateLimits["github"]
	} else {
		rl = integrations.DefaultRateLimits["github_unauth"]
	}

	return &Client{
		Client:  integrations.NewClientWithRateLimit(backend, "github:", cacheTTL, headers, rl.RequestsPerSecond, rl.Burst),
		baseURL: "https://api.github.com",
	}
}

// Fetch retrieves repository metrics (stars, maintainers, activity) from GitHub.
//
// Parameters:
//   - owner: Repository owner username (e.g., "pallets")
//   - repo: Repository name (e.g., "flask")
//   - refresh: If true, bypass cache and fetch fresh data
//
// The method performs up to 3 API calls:
//  1. Repository metadata (required)
//  2. Latest release data (optional, silently ignored if no releases)
//  3. Contributors list (optional, top 5, silently ignored on failure)
//
// If refresh is true, the cache is bypassed and a fresh API call is made.
// If refresh is false, cached data is returned if available and not expired.
//
// Returns:
//   - RepoMetrics populated with repository data on success
//   - [integrations.ErrNotFound] if the repository doesn't exist
//   - [integrations.ErrNetwork] for HTTP failures (timeout, 5xx, rate limits, etc.)
//   - Other errors for JSON decoding failures
//
// The returned RepoMetrics pointer is never nil if err is nil.
// This method is safe for concurrent use.
func (c *Client) Fetch(ctx context.Context, owner, repo string, refresh bool) (*integrations.RepoMetrics, error) {
	key := owner + "/" + repo

	var m integrations.RepoMetrics
	err := c.Cached(ctx, key, refresh, &m, func() error {
		return c.fetchMetrics(ctx, owner, repo, &m)
	})
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) fetchMetrics(ctx context.Context, owner, repo string, m *integrations.RepoMetrics) error {
	data, err := c.fetchRepo(ctx, owner, repo)
	if err != nil {
		return err
	}

	*m = integrations.RepoMetrics{
		RepoURL:     fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		Owner:       owner,
		Description: data.Description,
		Stars:       data.Stars,
		SizeKB:      data.Size,
		License:     normalizeLicense(data.License.SPDXID),
		Language:    data.Language,
		Topics:      data.Topics,
		Archived:    data.Archived,
	}
	if data.PushedAt != nil {
		m.LastCommitAt = data.PushedAt
	}

	// Fetch release and contributors in parallel (both are optional/best-effort)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if rel, err := c.fetchRelease(ctx, owner, repo); err == nil {
			m.LastReleaseAt = &rel.PublishedAt
		}
	}()
	go func() {
		defer wg.Done()
		if contribs, err := c.fetchContributors(ctx, owner, repo); err == nil {
			m.Contributors = contribs
		}
	}()
	wg.Wait()

	return nil
}

func (c *Client) fetchRepo(ctx context.Context, owner, repo string) (*repoResponse, error) {
	var data repoResponse
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)
	if err := c.Get(ctx, url, &data); err != nil {
		if errors.Is(err, integrations.ErrNotFound) {
			return nil, fmt.Errorf("%w: github repo %s/%s", err, owner, repo)
		}
		return nil, err
	}
	return &data, nil
}

func (c *Client) fetchRelease(ctx context.Context, owner, repo string) (*releaseResponse, error) {
	var data releaseResponse
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, owner, repo)
	if err := c.Get(ctx, url, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) fetchContributors(ctx context.Context, owner, repo string) ([]integrations.Contributor, error) {
	var data []contributorResponse
	url := fmt.Sprintf("%s/repos/%s/%s/contributors?per_page=5", c.baseURL, owner, repo)
	if err := c.Get(ctx, url, &data); err != nil {
		return nil, err
	}

	var result []integrations.Contributor
	for _, cr := range data {
		if cr.Type != "Bot" {
			result = append(result, integrations.Contributor{
				Login:         cr.Login,
				Contributions: cr.Contributions,
			})
		}
	}
	return result, nil
}

// ExtractURL extracts GitHub repository owner and name from package URLs.
//
// This function searches through urls map and homepage for GitHub URLs.
// It looks for patterns like "https://github.com/owner/repo".
//
// Parameters:
//   - urls: Map of URL keys to URL values from package metadata (may be nil)
//   - homepage: Fallback homepage URL (may be empty)
//
// Returns:
//   - owner: Repository owner username (empty if not found)
//   - repo: Repository name (empty if not found)
//   - ok: true if a GitHub URL was found, false otherwise
//
// This function is safe for concurrent use.
func ExtractURL(urls map[string]string, homepage string) (owner, repo string, ok bool) {
	return integrations.ExtractRepoURL(repoURLPattern, urls, homepage)
}

type repoResponse struct {
	Description string     `json:"description"`
	Stars       int        `json:"stargazers_count"`
	Size        int        `json:"size"`
	PushedAt    *time.Time `json:"pushed_at"`
	License     struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Language string   `json:"language"`
	Topics   []string `json:"topics"`
	Archived bool     `json:"archived"`
}

type releaseResponse struct {
	PublishedAt time.Time `json:"published_at"`
}

type contributorResponse struct {
	Login         string `json:"login"`
	Contributions int    `json:"contributions"`
	Type          string `json:"type"`
}

// normalizeLicense filters out meaningless license values from the GitHub API.
// GitHub returns "NOASSERTION" when it can't detect the license from the LICENSE file,
// which would incorrectly overwrite valid license data from package registries.
func normalizeLicense(spdxID string) string {
	switch spdxID {
	case "NOASSERTION", "OTHER", "":
		// NOASSERTION = GitHub couldn't determine the license
		// OTHER = Detected but not a standard SPDX license
		// Empty = No license info
		return ""
	default:
		return spdxID
	}
}
