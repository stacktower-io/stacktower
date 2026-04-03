// Package github provides an HTTP client for the GitHub API.
//
// # Overview
//
// This package fetches repository metrics from GitHub (https://api.github.com)
// for metadata enrichment. It provides data used by Nebraska ranking and
// brittle detection features.
//
// # Usage
//
//	client, err := github.NewClient(token, 24 * time.Hour)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	metrics, err := client.Fetch(ctx, "pallets", "flask", false)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println("Stars:", metrics.Stars)
//	fmt.Println("Contributors:", metrics.Contributors)
//
// # Authentication
//
// A GitHub personal access token is optional but recommended to avoid rate
// limits. Without a token, the client is limited to 60 requests/hour.
// With a token, the limit is 5000 requests/hour.
//
// # RepoMetrics
//
// [Fetch] returns an [integrations.RepoMetrics] containing:
//
//   - Stars: Stargazer count
//   - Owner: Repository owner login
//   - Contributors: Top 5 contributors with commit counts
//   - LastCommitAt: Most recent push date
//   - LastReleaseAt: Most recent release date
//   - Archived: Whether the repository is archived
//   - License, Language, Topics: Additional metadata
//
// # Caching
//
// Responses are cached to reduce API calls. The cache TTL is set when
// creating the client. Pass refresh=true to bypass the cache.
//
// # URL Extraction
//
// [ExtractURL] parses GitHub repository URLs from package metadata,
// handling various URL formats (with/without .git, trailing slashes, etc.).
package github
