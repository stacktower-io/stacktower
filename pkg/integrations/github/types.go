package github

import "time"

// User represents a GitHub user.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email"`
}

// Repo represents a GitHub repository.
type Repo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	Language      string `json:"language"`
	UpdatedAt     string `json:"updated_at"`
}

// ContentItem represents an item in a repository directory listing.
type ContentItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "dir"
	Size int    `json:"size"`
}

// FileContent represents the content of a file.
type FileContent struct {
	Path    string `json:"path"`
	Size    int    `json:"size"`
	Content string `json:"content"`
}

// ManifestFile represents a detected manifest file.
type ManifestFile struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Name     string `json:"name"`
}

// RepoWithManifests holds a repo and its detected manifest files.
type RepoWithManifests struct {
	Repo      Repo           `json:"repo"`
	Manifests []ManifestFile `json:"manifests,omitempty"`
}

// Org represents a GitHub organization.
type Org struct {
	ID          int64  `json:"id"`
	Login       string `json:"login"`
	Description string `json:"description"`
	AvatarURL   string `json:"avatar_url"`
}

// OrgMembership represents a user's membership in a GitHub organization.
// Returned by GET /user/memberships/orgs.
type OrgMembership struct {
	State string `json:"state"` // "active" or "pending"
	Role  string `json:"role"`  // "admin" or "member"
	Org   Org    `json:"organization"`
}

// OAuthConfig holds OAuth configuration.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// OAuthToken represents an OAuth access token response.
type OAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// apiRepoResponse is the internal GitHub API response structure.
type apiRepoResponse struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FullName      string     `json:"full_name"`
	Description   string     `json:"description"`
	Private       bool       `json:"private"`
	DefaultBranch string     `json:"default_branch"`
	Language      string     `json:"language"`
	UpdatedAt     string     `json:"updated_at"`
	Stars         int        `json:"stargazers_count"`
	Forks         int        `json:"forks_count"`
	OpenIssues    int        `json:"open_issues_count"`
	Size          int        `json:"size"`
	PushedAt      *time.Time `json:"pushed_at"`
	License       struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Topics   []string `json:"topics"`
	Archived bool     `json:"archived"`
}

// apiContentResponse is the internal GitHub API response for file content.
type apiContentResponse struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int    `json:"size"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}
