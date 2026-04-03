package deps_test

import (
	"fmt"
	"time"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func ExampleOptions_WithDefaults() {
	// Create options with some custom values
	opts := deps.Options{
		MaxDepth: 10,
		// MaxNodes and CacheTTL left as zero - will get defaults
	}

	// Apply defaults to fill in missing values
	opts = opts.WithDefaults()

	fmt.Println("MaxDepth:", opts.MaxDepth)
	fmt.Println("MaxNodes:", opts.MaxNodes)
	fmt.Println("CacheTTL:", opts.CacheTTL)
	// Output:
	// MaxDepth: 10
	// MaxNodes: 5000
	// CacheTTL: 24h0m0s
}

func ExamplePackage_Metadata() {
	// Package metadata from a registry
	pkg := deps.Package{
		Name:        "fastapi",
		Version:     "0.100.0",
		Description: "FastAPI framework, high performance",
		License:     "MIT",
		Author:      "Sebastián Ramírez",
		Downloads:   1000000,
	}

	// Convert to node metadata map
	meta := pkg.Metadata()

	fmt.Println("version:", meta["version"])
	fmt.Println("license:", meta["license"])
	fmt.Println("has description:", meta["description"] != nil)
	// Output:
	// version: 0.100.0
	// license: MIT
	// has description: true
}

func ExamplePackage_Ref() {
	// Package with repository information
	pkg := deps.Package{
		Name:       "requests",
		Version:    "2.31.0",
		Repository: "https://github.com/psf/requests",
		HomePage:   "https://requests.readthedocs.io",
	}

	// Create a reference for metadata enrichment lookups
	ref := pkg.Ref()

	fmt.Println("Name:", ref.Name)
	fmt.Println("Repository URL:", ref.ProjectURLs["repository"])
	// Output:
	// Name: requests
	// Repository URL: https://github.com/psf/requests
}

func ExampleDetectManifest() {
	// DetectManifest finds the right parser for a manifest file.
	// In real usage, you would get parsers from a Language definition:
	//
	//   import "github.com/matzehuels/stacktower/pkg/core/deps/python"
	//
	//   parsers := python.Language.ManifestParsers(nil)
	//   parser, err := deps.DetectManifest("poetry.lock", parsers...)
	//   if err != nil {
	//       log.Fatal(err)
	//   }
	//   result, err := parser.Parse("poetry.lock", opts)
	//
	// The function matches filename patterns to find a suitable parser.
	// Returns an error if no parser recognizes the file.

	fmt.Println("DetectManifest matches filename to parser type")
	// Output:
	// DetectManifest matches filename to parser type
}

func Example_constants() {
	// Default values for dependency resolution
	fmt.Println("DefaultMaxDepth:", deps.DefaultMaxDepth)
	fmt.Println("DefaultMaxNodes:", deps.DefaultMaxNodes)
	fmt.Println("DefaultCacheTTL:", deps.DefaultCacheTTL)
	// Output:
	// DefaultMaxDepth: 50
	// DefaultMaxNodes: 5000
	// DefaultCacheTTL: 24h0m0s
}

func ExampleOptions_withLogger() {
	// Custom logger for progress tracking
	var logs []string
	opts := deps.Options{
		MaxDepth: 5,
		Logger: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	}.WithDefaults()

	// Logger is preserved through WithDefaults
	opts.Logger("Fetching %s", "fastapi")
	opts.Logger("Found %d dependencies", 12)

	fmt.Println("Log count:", len(logs))
	// Output:
	// Log count: 2
}

func ExampleOptions_limits() {
	// Configure resolution limits for large dependency trees
	opts := deps.Options{
		MaxDepth: 10,  // Stop at 10 levels deep
		MaxNodes: 100, // Fetch at most 100 packages
		CacheTTL: time.Hour,
		Refresh:  true, // Bypass cache for fresh data
	}

	fmt.Println("MaxDepth:", opts.MaxDepth)
	fmt.Println("MaxNodes:", opts.MaxNodes)
	fmt.Println("Refresh:", opts.Refresh)
	// Output:
	// MaxDepth: 10
	// MaxNodes: 100
	// Refresh: true
}

func ExampleNewPubGrubResolver() {
	// NewPubGrubResolver wraps a Fetcher with SAT-solver-based resolution.
	// In real usage, you would pass a registry-specific fetcher and
	// constraint parser:
	//
	//   resolver, err := deps.NewPubGrubResolver("pypi", fetcher, parser)
	//   g, err := resolver.Resolve(ctx, "requests", deps.Options{})
	//
	// The resolver uses PubGrub for proper version constraint solving
	// with conflict detection and backtracking, then enriches metadata
	// in parallel.

	fmt.Println("NewPubGrubResolver creates a SAT-solver dependency resolver")
	// Output:
	// NewPubGrubResolver creates a SAT-solver dependency resolver
}
