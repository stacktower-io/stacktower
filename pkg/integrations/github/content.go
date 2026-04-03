package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// defaultTimeout is the HTTP client timeout for GitHub API requests.
	defaultTimeout = 30 * time.Second

	// maxPages is the maximum number of pages to fetch for paginated endpoints.
	// With 100 items per page, this caps results at 1000 items.
	maxPages = 10

	// maxConcurrentScans is the maximum concurrent goroutines for scanning repos.
	maxConcurrentScans = 10
)

// ContentClient provides access to GitHub repository content.
// Use this for fetching files, listing directories, and user operations.
type ContentClient struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// apiError builds a human-readable error from a GitHub API error response.
// It extracts the "message" field from the JSON body when available,
// and maps common status codes to friendly descriptions.
func apiError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		return fmt.Errorf("GitHub API: %s (HTTP %d)", parsed.Message, resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub API: bad credentials — run 'stacktower github login' to re-authenticate (HTTP 401)")
	case http.StatusForbidden:
		return fmt.Errorf("GitHub API: forbidden — token may lack required scopes (HTTP 403)")
	case http.StatusNotFound:
		return fmt.Errorf("GitHub API: not found (HTTP 404)")
	default:
		return fmt.Errorf("GitHub API error (HTTP %d)", resp.StatusCode)
	}
}

// NewContentClient creates a new content client with the given access token.
func NewContentClient(token string) *ContentClient {
	return &ContentClient{
		token:      token,
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    "https://api.github.com",
	}
}

// FetchUser retrieves the authenticated user's info.
func (c *ContentClient) FetchUser(ctx context.Context) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// If the user has no public email, try to fetch their primary verified email
	// via GET /user/emails (requires the user:email scope).
	if user.Email == "" {
		if email, err := c.fetchPrimaryEmail(ctx); err == nil && email != "" {
			user.Email = email
		}
	}

	return &user, nil
}

// fetchPrimaryEmail retrieves the user's primary verified email address.
// GitHub only exposes emails through GET /user/emails, not through GET /user
// (which only returns the email if the user has set it as public).
func (c *ContentClient) fetchPrimaryEmail(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/user/emails", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", apiError(resp)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	// Return the primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	// Fall back to any verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", nil
}

// FetchUserOrgs retrieves the authenticated user's active GitHub organization memberships.
// Returns only orgs where the membership state is "active" (ignores pending invitations).
// Each result includes the user's role in the org ("admin" or "member").
func (c *ContentClient) FetchUserOrgs(ctx context.Context) ([]OrgMembership, error) {
	var all []OrgMembership
	page := 1

	for {
		url := fmt.Sprintf("%s/user/memberships/orgs?state=active&per_page=100&page=%d", c.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			return nil, err
		}

		var memberships []OrgMembership
		if err := json.NewDecoder(resp.Body).Decode(&memberships); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		if len(memberships) == 0 {
			break
		}

		all = append(all, memberships...)
		page++

		if page > maxPages {
			break // Safety limit
		}
	}

	return all, nil
}

// FetchUserRepos retrieves all of the authenticated user's repositories.
// This includes private repos if the OAuth token has the 'repo' scope.
// Results are paginated automatically to retrieve all repos.
func (c *ContentClient) FetchUserRepos(ctx context.Context) ([]Repo, error) {
	var allRepos []Repo
	page := 1

	for {
		url := fmt.Sprintf("%s/user/repos?sort=updated&per_page=100&page=%d", c.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			return nil, err
		}

		var repos []Repo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		if len(repos) == 0 {
			break // No more pages
		}

		allRepos = append(allRepos, repos...)
		page++

		// Safety limit to avoid infinite loops
		if page > maxPages {
			break
		}
	}

	return allRepos, nil
}

// ListContents lists files and directories in a repository path.
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
func (c *ContentClient) ListContents(ctx context.Context, owner, repo, path, ref string) ([]ContentItem, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)
	if ref != "" {
		url += "?ref=" + ref
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var items []apiContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := make([]ContentItem, len(items))
	for i, item := range items {
		result[i] = ContentItem{
			Name: item.Name,
			Path: item.Path,
			Type: item.Type,
			Size: item.Size,
		}
	}

	return result, nil
}

// FetchFile retrieves the content of a file from a repository.
// The content is returned as a string (decoded from base64).
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
func (c *ContentClient) FetchFile(ctx context.Context, owner, repo, path, ref string) (*FileContent, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)
	if ref != "" {
		url += "?ref=" + ref
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var fileResp apiContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Decode base64 content
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(fileResp.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}

	return &FileContent{
		Path:    fileResp.Path,
		Size:    fileResp.Size,
		Content: string(content),
	}, nil
}

// FetchFileRaw retrieves the raw content of a file from a repository.
// This is more efficient for large files as it doesn't use base64 encoding.
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
func (c *ContentClient) FetchFileRaw(ctx context.Context, owner, repo, path, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", c.baseURL, owner, repo, path)
	if ref != "" {
		url += "?ref=" + ref
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3.raw") // Get raw content

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", apiError(resp)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read content: %w", err)
	}

	return string(content), nil
}

// DetectManifests finds manifest files in a repository's root directory.
// The patterns map filename -> language name (e.g., "go.mod" -> "go").
// Use deps.SupportedManifests(languages) to get patterns from the deps package.
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
func (c *ContentClient) DetectManifests(ctx context.Context, owner, repo, ref string, patterns map[string]string) ([]ManifestFile, error) {
	items, err := c.ListContents(ctx, owner, repo, "", ref)
	if err != nil {
		return nil, err
	}

	var manifests []ManifestFile
	for _, item := range items {
		if item.Type == "file" {
			if lang, ok := patterns[item.Name]; ok {
				manifests = append(manifests, ManifestFile{
					Path:     item.Path,
					Language: lang,
					Name:     item.Name,
				})
			}
		}
	}

	return manifests, nil
}

// setHeaders sets common headers for GitHub API requests.
func (c *ContentClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// SearchCode searches for code in a repository.
// Query follows GitHub code search syntax: https://docs.github.com/en/search-github/searching-on-github/searching-code
func (c *ContentClient) SearchCode(ctx context.Context, owner, repo, query string) ([]CodeSearchResult, error) {
	// Build search query with repo filter
	fullQuery := fmt.Sprintf("%s repo:%s/%s", query, owner, repo)
	url := fmt.Sprintf("%s/search/code?q=%s&per_page=20", c.baseURL, urlEncode(fullQuery))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var searchResp codeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]CodeSearchResult, len(searchResp.Items))
	for i, item := range searchResp.Items {
		results[i] = CodeSearchResult{
			Name: item.Name,
			Path: item.Path,
		}
		// Extract text matches if available
		for _, match := range item.TextMatches {
			results[i].Matches = append(results[i].Matches, match.Fragment)
		}
	}

	return results, nil
}

// GetTree retrieves the full file tree of a repository.
func (c *ContentClient) GetTree(ctx context.Context, owner, repo, branch string) ([]TreeEntry, error) {
	if branch == "" {
		branch = "HEAD"
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", c.baseURL, owner, repo, branch)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var treeResp treeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	entries := make([]TreeEntry, 0, len(treeResp.Tree))
	for _, item := range treeResp.Tree {
		entries = append(entries, TreeEntry{
			Path: item.Path,
			Type: item.Type,
			Size: item.Size,
		})
	}

	return entries, nil
}

// GetRepoInfo retrieves repository metadata.
func (c *ContentClient) GetRepoInfo(ctx context.Context, owner, repo string) (*RepoInfo, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var repoResp apiRepoResponse
	if err := json.NewDecoder(resp.Body).Decode(&repoResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &RepoInfo{
		Name:          repoResp.Name,
		FullName:      repoResp.FullName,
		Description:   repoResp.Description,
		Language:      repoResp.Language,
		DefaultBranch: repoResp.DefaultBranch,
		Stars:         repoResp.Stars,
		Forks:         repoResp.Forks,
		OpenIssues:    repoResp.OpenIssues,
		License:       normalizeLicense(repoResp.License.SPDXID),
		Topics:        repoResp.Topics,
		Archived:      repoResp.Archived,
		Private:       repoResp.Private,
	}, nil
}

// ListBranches retrieves branches for a repository.
// Results are capped at maxPages * 100 items; large repos may be truncated.
func (c *ContentClient) ListBranches(ctx context.Context, owner, repo string) ([]Branch, error) {
	var allBranches []Branch
	page := 1

	for {
		url := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=100&page=%d", c.baseURL, owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			return nil, err
		}

		var branches []apiBranchResponse
		if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		if len(branches) == 0 {
			break
		}

		for _, b := range branches {
			allBranches = append(allBranches, Branch{
				Name:      b.Name,
				Commit:    b.Commit.SHA,
				Protected: b.Protected,
			})
		}

		page++
		if page > maxPages {
			break // Safety limit
		}
	}

	return allBranches, nil
}

// ListTags retrieves tags for a repository.
// Results are capped at maxPages * 100 items; large repos may be truncated.
func (c *ContentClient) ListTags(ctx context.Context, owner, repo string) ([]Tag, error) {
	var allTags []Tag
	page := 1

	for {
		url := fmt.Sprintf("%s/repos/%s/%s/tags?per_page=100&page=%d", c.baseURL, owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("send request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			return nil, err
		}

		var tags []apiTagResponse
		if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		if len(tags) == 0 {
			break
		}

		for _, t := range tags {
			allTags = append(allTags, Tag{
				Name:   t.Name,
				Commit: t.Commit.SHA,
			})
		}

		page++
		if page > maxPages {
			break // Safety limit
		}
	}

	return allTags, nil
}

// ResolveVersionToRef finds the git ref that best matches a package version.
// It tries common tag patterns: bare version ("3.1.2"), "v"-prefixed ("v3.1.2"),
// and repo-prefixed variants ("flask-3.1.2", "flask_3.1.2").
// Falls back to the repository's default branch if no tag matches.
//
// This is useful for mapping a package version (from PyPI, npm, etc.) to the
// corresponding git ref for source code analysis.
//
// Parameters:
//   - owner: Repository owner (e.g., "pallets")
//   - repo: Repository name (e.g., "flask")
//   - version: Package version to match (e.g., "3.1.2")
//
// Returns the matching tag name, or the default branch if no tag matches.
func (c *ContentClient) ResolveVersionToRef(ctx context.Context, owner, repo, version string) (string, error) {
	tags, err := c.ListTags(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("list tags: %w", err)
	}

	if version != "" {
		// Build tag set for fast lookup
		tagSet := make(map[string]bool, len(tags))
		for _, t := range tags {
			tagSet[t.Name] = true
		}

		// Try common tag patterns in preference order
		repoLower := strings.ToLower(repo)
		candidates := []string{
			version,
			"v" + version,
			repoLower + "-" + version,
			repoLower + "_" + version,
			repoLower + "/v" + version,
		}

		for _, c := range candidates {
			if tagSet[c] {
				return c, nil
			}
		}
	}

	// No matching tag found — fall back to default branch
	info, err := c.GetRepoInfo(ctx, owner, repo)
	if err != nil {
		return "main", nil // best-effort fallback
	}
	if info.DefaultBranch != "" {
		return info.DefaultBranch, nil
	}
	return "main", nil
}

// GetReadme retrieves the README file for a repository.
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
// Returns nil, nil if no README is found.
func (c *ContentClient) GetReadme(ctx context.Context, owner, repo, ref string) (*FileContent, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/readme", c.baseURL, owner, repo)
	if ref != "" {
		url += "?ref=" + ref
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// 404 means no README found - not an error
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var readmeResp apiContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&readmeResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Decode base64 content
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readmeResp.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}

	return &FileContent{
		Path:    readmeResp.Path,
		Size:    readmeResp.Size,
		Content: string(content),
	}, nil
}

// DetectManifestsRecursive finds all manifest files in a repository using the Git tree API.
// This is useful for monorepos with nested manifest files.
// The patterns map filename -> language name (e.g., "go.mod" -> "go").
// If ref is non-empty, it specifies a branch, tag, or commit SHA.
func (c *ContentClient) DetectManifestsRecursive(ctx context.Context, owner, repo, ref string, patterns map[string]string) ([]ManifestFile, error) {
	if ref == "" {
		ref = "HEAD"
	}

	entries, err := c.GetTree(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}

	var manifests []ManifestFile
	for _, entry := range entries {
		if entry.Type != "blob" {
			continue // Skip directories
		}

		// Extract filename from path
		name := entry.Path
		if idx := strings.LastIndex(entry.Path, "/"); idx >= 0 {
			name = entry.Path[idx+1:]
		}

		if lang, ok := patterns[name]; ok {
			manifests = append(manifests, ManifestFile{
				Path:     entry.Path,
				Language: lang,
				Name:     name,
			})
		}
	}

	return manifests, nil
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// Branch represents a Git branch in a repository.
type Branch struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"` // SHA of the branch HEAD
	Protected bool   `json:"protected"`
}

// Tag represents a Git tag in a repository.
type Tag struct {
	Name   string `json:"name"`
	Commit string `json:"commit"` // SHA of the tagged commit
}

// CodeSearchResult represents a code search match.
type CodeSearchResult struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Matches []string `json:"matches,omitempty"`
}

// TreeEntry represents a file or directory in the repository tree.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	Size int    `json:"size,omitempty"`
}

// RepoInfo contains repository metadata.
type RepoInfo struct {
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	Language      string   `json:"language"`
	DefaultBranch string   `json:"default_branch"`
	Stars         int      `json:"stars"`
	Forks         int      `json:"forks"`
	OpenIssues    int      `json:"open_issues"`
	License       string   `json:"license"`
	Topics        []string `json:"topics"`
	Archived      bool     `json:"archived"`
	Private       bool     `json:"private"`
}

type codeSearchResponse struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		TextMatches []struct {
			Fragment string `json:"fragment"`
		} `json:"text_matches"`
	} `json:"items"`
}

type treeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int    `json:"size"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

type apiBranchResponse struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
	Protected bool `json:"protected"`
}

type apiTagResponse struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// DetectLanguageFromManifest determines the language from a manifest filename.
func DetectLanguageFromManifest(path string) string {
	name := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		name = path[idx+1:]
	}

	switch name {
	case "package.json", "package-lock.json":
		return "javascript"
	case "requirements.txt", "setup.py", "pyproject.toml", "Pipfile":
		return "python"
	case "Cargo.toml":
		return "rust"
	case "go.mod":
		return "go"
	case "Gemfile":
		return "ruby"
	case "composer.json":
		return "php"
	case "pom.xml", "build.gradle":
		return "java"
	default:
		return ""
	}
}

// ScanReposForManifests fetches repos and detects manifests in parallel.
// It returns repos sorted by UpdatedAt (most recent first).
// The manifestPatterns map should be filename -> language (use deps.SupportedManifests).
// Set publicOnly=true to filter out private repos.
func (c *ContentClient) ScanReposForManifests(ctx context.Context, manifestPatterns map[string]string, publicOnly bool) ([]RepoWithManifests, error) {
	repos, err := c.FetchUserRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch repos: %w", err)
	}

	// Sort by most recently updated
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].UpdatedAt > repos[j].UpdatedAt
	})

	// Filter private repos if requested
	if publicOnly {
		var filtered []Repo
		for _, r := range repos {
			if !r.Private {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}

	if len(repos) == 0 {
		return nil, nil
	}

	// Parallel manifest detection with worker pool
	type repoResult struct {
		idx       int
		repo      Repo
		manifests []ManifestFile
	}

	results := make([]repoResult, len(repos))
	var wg sync.WaitGroup

	// Semaphore for concurrency limit (10 parallel requests)
	sem := make(chan struct{}, maxConcurrentScans)

	for i, r := range repos {
		wg.Add(1)
		go func(idx int, repo Repo) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			parts := strings.SplitN(repo.FullName, "/", 2)
			var manifests []ManifestFile
			if len(parts) == 2 {
				manifests, _ = c.DetectManifests(ctx, parts[0], parts[1], "", manifestPatterns)
			}
			results[idx] = repoResult{idx: idx, repo: repo, manifests: manifests}
		}(i, r)
	}

	wg.Wait()

	// Build final list preserving order
	rwm := make([]RepoWithManifests, len(repos))
	for _, r := range results {
		rwm[r.idx] = RepoWithManifests{Repo: r.repo, Manifests: r.manifests}
	}

	return rwm, nil
}

// =============================================================================
// GitHub App Installation
// =============================================================================

// AppInstallation represents a GitHub App installation accessible to the user.
type AppInstallation struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"app_id"`
	AppSlug string `json:"app_slug"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"` // "User" or "Organization"
	} `json:"account"`
	RepositorySelection string `json:"repository_selection"` // "all" or "selected"
}

// GetAppInstallations lists all GitHub App installations accessible to the authenticated user.
// This can be used to check if the user has installed a specific GitHub App.
func (c *ContentClient) GetAppInstallations(ctx context.Context) ([]AppInstallation, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/user/installations", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var result struct {
		TotalCount    int               `json:"total_count"`
		Installations []AppInstallation `json:"installations"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Installations, nil
}

// HasAppInstallation checks if the user has installed a GitHub App with the given slug.
// Returns the installation if found, or nil if not installed.
func (c *ContentClient) HasAppInstallation(ctx context.Context, appSlug string) (*AppInstallation, error) {
	installations, err := c.GetAppInstallations(ctx)
	if err != nil {
		return nil, err
	}

	for _, inst := range installations {
		if inst.AppSlug == appSlug {
			return &inst, nil
		}
	}

	return nil, nil
}
