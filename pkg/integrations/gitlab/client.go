package gitlab

import (
	"regexp"
	"strings"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/integrations"
)

// repoURLPattern captures the full project path so that nested groups
// (gitlab.com/group/subgroup/project) are preserved. Query strings and
// fragments are excluded from the capture.
var repoURLPattern = regexp.MustCompile(`https?://gitlab\.com/([^?#]+)`)

// Client provides access to the GitLab API for repository metadata enrichment.
// It handles HTTP requests with caching, automatic retries, and optional authentication.
//
// All methods are safe for concurrent use by multiple goroutines.
//
// Note: Full metrics fetching (stars, contributors, etc.) is not yet implemented.
// Currently, this client focuses on URL extraction. Use [ExtractURL] to identify GitLab-hosted packages.
type Client struct {
	*integrations.Client
}

// NewClient creates a GitLab API client with optional authentication.
//
// Parameters:
//   - backend: Cache backend for HTTP response caching (use storage.NullBackend{} for no caching)
//   - token: GitLab personal access token (empty string for unauthenticated)
//   - cacheTTL: How long responses are cached (typical: 1-24 hours)
//
// The returned Client is safe for concurrent use.
func NewClient(backend cache.Cache, token string, cacheTTL time.Duration) *Client {
	var headers map[string]string
	if token != "" {
		headers = map[string]string{"PRIVATE-TOKEN": token}
	}

	return &Client{integrations.NewClient(backend, "gitlab:", cacheTTL, headers)}
}

// ExtractURL extracts GitLab repository owner and name from package URLs.
//
// This function searches through urls map and homepage for GitLab URLs.
// It looks for patterns like "https://gitlab.com/owner/repo".
//
// GitLab supports nested groups: for "https://gitlab.com/group/subgroup/project"
// the project is the last path segment and the namespace is everything before
// it (owner="group/subgroup", repo="project"). A trailing ".git" suffix is
// stripped. URLs with "/-/" paths (file/tree/issues views) are ignored since
// the project boundary cannot be reliably determined from them.
//
// Parameters:
//   - urls: Map of URL keys to URL values from package metadata (may be nil)
//   - homepage: Fallback homepage URL (may be empty)
//
// Returns:
//   - owner: Repository namespace, possibly containing "/" for nested groups
//     (empty if not found)
//   - repo: Repository name (empty if not found)
//   - ok: true if a GitLab URL was found, false otherwise
//
// This function is safe for concurrent use.
func ExtractURL(urls map[string]string, homepage string) (owner, repo string, ok bool) {
	return integrations.ExtractRepoURLFunc(matchGitLabURL, urls, homepage)
}

// matchGitLabURL parses a single URL and extracts the GitLab namespace and
// project name, treating the last path segment as the project and everything
// before it as the (possibly nested) namespace.
func matchGitLabURL(u string) (owner, repo string, ok bool) {
	m := repoURLPattern.FindStringSubmatch(u)
	if m == nil {
		return "", "", false
	}
	path := strings.Trim(m[1], "/")

	// "/-/" separates the project path from sub-resources (blob, tree,
	// issues, ...); such URLs are skipped because the project boundary is
	// ambiguous for callers expecting a plain repository URL.
	if strings.Contains("/"+path+"/", "/-/") {
		return "", "", false
	}

	path = strings.TrimSuffix(path, ".git")
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return "", "", false
	}
	for _, s := range segments {
		if s == "" {
			return "", "", false
		}
	}

	repo = segments[len(segments)-1]
	owner = strings.Join(segments[:len(segments)-1], "/")
	return owner, repo, true
}
