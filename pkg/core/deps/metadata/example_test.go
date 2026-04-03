package metadata_test

import (
	"context"
	"fmt"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/core/deps/python"
)

func ExampleNewGitHub() {
	// Create a GitHub metadata provider with authentication
	// Use an empty string for unauthenticated requests (lower rate limits)
	token := "" // or os.Getenv("GITHUB_TOKEN")
	provider := metadata.NewGitHub(cache.NewNullCache(), token, 24*time.Hour)

	// Use with resolver options
	opts := deps.Options{
		MetadataProviders: []deps.MetadataProvider{provider},
		MaxDepth:          5,
		MaxNodes:          100,
	}

	resolver, _ := python.Language.Resolver(cache.NewNullCache(), opts)
	ctx := context.Background()
	g, err := resolver.Resolve(ctx, "requests", opts)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Resolved %d packages with GitHub metadata\n", g.NodeCount())
	// Output varies based on network and API availability
}

func ExampleGitHub_Enrich() {
	// Enrich a single package with GitHub metadata
	token := "" // or os.Getenv("GITHUB_TOKEN")
	provider := metadata.NewGitHub(cache.NewNullCache(), token, 24*time.Hour)

	// Package reference with GitHub URL
	pkg := &deps.PackageRef{
		Name:     "requests",
		HomePage: "https://github.com/psf/requests",
	}

	ctx := context.Background()
	meta, err := provider.Enrich(ctx, pkg, false)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	if meta != nil {
		fmt.Println("Enriched with GitHub data")
		if stars, ok := meta[metadata.RepoStars].(int); ok {
			fmt.Printf("Stars: %d\n", stars)
		}
		if owner, ok := meta[metadata.RepoOwner].(string); ok {
			fmt.Printf("Owner: %s\n", owner)
		}
	}
	// Output varies based on network and API availability
}

func ExampleNewComposite() {
	// Combine multiple metadata providers
	github := metadata.NewGitHub(cache.NewNullCache(), "", 24*time.Hour)

	// Composite merges results from all providers
	composite := metadata.NewComposite(github)

	// Use in resolver options
	opts := deps.Options{
		MetadataProviders: []deps.MetadataProvider{composite},
		MaxDepth:          3,
		MaxNodes:          50,
	}

	resolver, _ := python.Language.Resolver(cache.NewNullCache(), opts)
	ctx := context.Background()
	g, err := resolver.Resolve(ctx, "flask", opts)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Resolved with composite metadata provider\n")
	fmt.Printf("Packages: %d\n", g.NodeCount())
	// Output varies based on network and API availability
}

func Example_metadataKeys() {
	// Demonstrate standard metadata keys
	fmt.Println("Standard metadata keys:")
	fmt.Printf("  %s: Repository URL\n", metadata.RepoURL)
	fmt.Printf("  %s: Repository owner\n", metadata.RepoOwner)
	fmt.Printf("  %s: Star count\n", metadata.RepoStars)
	fmt.Printf("  %s: Archived status\n", metadata.RepoArchived)
	fmt.Printf("  %s: Maintainer list\n", metadata.RepoMaintainers)
	fmt.Printf("  %s: Last commit date\n", metadata.RepoLastCommit)
	fmt.Printf("  %s: Last release date\n", metadata.RepoLastRelease)
	fmt.Printf("  %s: Primary language\n", metadata.RepoLanguage)
	fmt.Printf("  %s: Topic tags\n", metadata.RepoTopics)
	// Output:
	// Standard metadata keys:
	//   repo_url: Repository URL
	//   repo_owner: Repository owner
	//   repo_stars: Star count
	//   repo_archived: Archived status
	//   repo_maintainers: Maintainer list
	//   repo_last_commit: Last commit date
	//   repo_last_release: Last release date
	//   repo_language: Primary language
	//   repo_topics: Topic tags
}
