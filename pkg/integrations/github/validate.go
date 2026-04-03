package github

import (
	"errors"
	"regexp"
	"strings"
)

// Regex patterns for GitHub resource validation.
var (
	// GitHub usernames/orgs: 1-39 alphanumeric or hyphen, not starting with hyphen
	validOwner = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
	// GitHub repo names: 1-100 alphanumeric, hyphen, underscore, or dot
	validRepo = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,100}$`)
)

// ValidateOwner validates a GitHub username or organization name.
func ValidateOwner(owner string) error {
	if owner == "" {
		return errors.New("owner is required")
	}
	if !validOwner.MatchString(owner) {
		return errors.New("invalid owner format: must be 1-39 alphanumeric characters or hyphens, cannot start with hyphen")
	}
	return nil
}

// ValidateRepo validates a GitHub repository name.
func ValidateRepo(repo string) error {
	if repo == "" {
		return errors.New("repo is required")
	}
	if !validRepo.MatchString(repo) {
		return errors.New("invalid repo format: must be 1-100 alphanumeric characters, hyphens, underscores, or dots")
	}
	return nil
}

// ValidateRepoRef validates both owner and repo parameters.
func ValidateRepoRef(owner, repo string) error {
	if err := ValidateOwner(owner); err != nil {
		return err
	}
	return ValidateRepo(repo)
}

// ParseRepoRef parses an "owner/repo" string and validates both parts.
// It also accepts full GitHub URLs in any of these forms:
//
//	https://github.com/owner/repo
//	http://github.com/owner/repo
//	github.com/owner/repo
//	owner/repo           (plain)
//
// Trailing slashes and ".git" suffixes are stripped automatically.
// Returns owner, repo, and any validation error.
func ParseRepoRef(ref string) (owner, repo string, err error) {
	ref = strings.TrimSuffix(strings.TrimRight(ref, "/"), ".git")

	// Strip scheme and host when a GitHub URL is supplied.
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(ref, prefix) {
			ref = strings.TrimPrefix(ref, prefix)
			break
		}
	}

	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", "", errors.New("invalid repo format: use owner/repo")
	}
	owner, repo = parts[0], parts[1]
	if err := ValidateRepoRef(owner, repo); err != nil {
		return "", "", err
	}
	return owner, repo, nil
}
