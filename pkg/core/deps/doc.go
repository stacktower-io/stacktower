// Package deps provides dependency resolution from package registries and
// manifest files.
//
// # Overview
//
// Package deps is the core abstraction layer for fetching dependency trees
// from multiple sources:
//
//   - Package registries: PyPI, npm, crates.io, RubyGems, Packagist, Maven, Go Proxy
//   - Manifest files: requirements.txt, package.json, Cargo.toml, poetry.lock, etc.
//
// It provides a concurrent resolver that crawls dependencies in parallel while
// respecting depth and node limits. The resulting dependency graphs are returned
// as [dag.DAG] structures suitable for visualization and analysis.
//
// This package powers the `stacktower parse` command and is language-agnostic,
// delegating language-specific details to subpackages (python, rust, javascript, etc.).
//
// # Architecture
//
// The dependency resolution system has three layers:
//
//  1. Registry integrations ([integrations]): HTTP clients for each registry API
//  2. Language definitions (this package): [Language] values that map registries and manifests
//  3. CLI ([internal/cli]): User commands like `stacktower parse`
//
// # Resolving Dependencies
//
// Use a [Language]'s resolver to fetch a complete dependency tree from a registry:
//
//	import "github.com/matzehuels/stacktower/pkg/core/deps/python"
//
//	resolver, _ := python.Language.Resolver()
//	g, _ := resolver.Resolve(ctx, "fastapi", deps.Options{
//	    MaxDepth: 10,
//	    MaxNodes: 1000,
//	})
//
// The resolver crawls dependencies concurrently:
//
//  1. Fetches the root package from the registry
//  2. Recursively fetches dependencies up to MaxDepth levels
//  3. Builds a [dag.DAG] with nodes for packages and edges for dependencies
//  4. Optionally enriches nodes with metadata from [MetadataProvider] sources
//
// # Options
//
// [Options] configures resolution behavior. All fields are optional and have
// sensible defaults via [Options.WithDefaults]:
//
//   - MaxDepth: Maximum dependency depth (default 50)
//   - MaxNodes: Maximum packages to fetch (default 5000)
//   - CacheTTL: HTTP cache duration (default 24h)
//   - Refresh: Bypass cache to force fresh data
//   - MetadataProviders: External enrichment sources (e.g., GitHub, GitLab)
//   - Logger: Progress and error callback (must be goroutine-safe)
//
// # Package Data
//
// Each resolved package is represented by a [Package] struct containing:
//
//   - Name, Version: Package identity from the registry
//   - Dependencies: Direct dependency names (recursively fetched)
//   - Description, License, Author: Registry metadata (availability varies)
//   - Repository, HomePage: Source code and documentation URLs
//   - Downloads: Popularity metric (when available from the registry)
//
// Use [Package.Metadata] to convert package fields to a map suitable for
// [dag.Node] metadata. Use [Package.Ref] to create a [PackageRef] for
// metadata provider lookups.
//
// # Manifest Parsing
//
// For local projects, parse dependency information directly from manifest files:
//
//	import "github.com/matzehuels/stacktower/pkg/core/deps/python"
//
//	parsers := python.Language.ManifestParsers(nil)
//	parser, _ := deps.DetectManifest("poetry.lock", parsers...)
//	result, _ := parser.Parse("poetry.lock", opts)
//	g := result.Graph
//
// Manifest parsers implement [ManifestParser] and vary in completeness:
//
//   - Direct dependencies only: requirements.txt, package.json (base)
//   - Full transitive closure: poetry.lock, Cargo.lock, package-lock.json
//
// Use [ManifestParser.IncludesTransitive] to check if additional resolution
// is needed. Use [DetectManifest] to find the right parser for a file.
//
// # Metadata Enrichment
//
// [MetadataProvider] implementations add supplementary data from external sources:
//
//	import "github.com/matzehuels/stacktower/pkg/core/deps/metadata"
//
//	providers := []deps.MetadataProvider{
//	    metadata.NewGitHubProvider(token, ttl),
//	}
//	opts := deps.Options{MetadataProviders: providers}
//
// The GitHub provider (see [metadata.GitHubProvider]) adds:
//   - repo_stars: GitHub star count
//   - repo_owner, repo_maintainers: Maintainer information
//   - repo_last_commit: Last commit timestamp
//   - repo_archived: Whether the repository is archived
//
// These fields power Nebraska ranking and brittle package detection.
//
// # Supported Languages
//
// Each language has a subpackage exporting a Language definition:
//
//   - [python]: PyPI registry, poetry.lock, requirements.txt, pyproject.toml
//   - [rust]: crates.io registry, Cargo.toml, Cargo.lock
//   - [javascript]: npm registry, package.json, package-lock.json
//   - [ruby]: RubyGems registry, Gemfile, Gemfile.lock
//   - [php]: Packagist registry, composer.json, composer.lock
//   - [java]: Maven Central registry, pom.xml
//   - [golang]: Go Module Proxy, go.mod
//
// Language subpackages also provide registry-specific [Fetcher] implementations
// that wrap HTTP clients from the [integrations] package.
//
// # Concurrency
//
// The resolver uses a worker pool (20 concurrent goroutines by default) to fetch
// packages in parallel. All public types are safe for concurrent use:
//
//   - [Fetcher.Fetch] must be goroutine-safe (called by multiple workers)
//   - [MetadataProvider.Enrich] must be goroutine-safe (called concurrently per package)
//   - [Options.Logger] must be goroutine-safe (called from multiple workers)
//
// Manifest parsers are not required to be goroutine-safe as they are typically
// called once per file.
//
// # Error Handling
//
// Resolution errors fall into two categories:
//
//   - Fatal: Root package not found or unreachable. [Resolver.Resolve] returns an error.
//   - Non-fatal: Transitive dependency failures. Logged via [Options.Logger] but don't fail resolution.
//
// Manifest parsing errors are always fatal and returned by [ManifestParser.Parse].
// Metadata enrichment errors are non-fatal and logged.
//
// [integrations]: github.com/matzehuels/stacktower/pkg/integrations
// [internal/cli]: github.com/matzehuels/stacktower/internal/cli
// [dag.DAG]: github.com/matzehuels/stacktower/pkg/core/dag.DAG
// [dag.Node]: github.com/matzehuels/stacktower/pkg/core/dag.Node
// [python]: github.com/matzehuels/stacktower/pkg/core/deps/python
// [rust]: github.com/matzehuels/stacktower/pkg/core/deps/rust
// [javascript]: github.com/matzehuels/stacktower/pkg/core/deps/javascript
// [ruby]: github.com/matzehuels/stacktower/pkg/core/deps/ruby
// [php]: github.com/matzehuels/stacktower/pkg/core/deps/php
// [java]: github.com/matzehuels/stacktower/pkg/core/deps/java
// [golang]: github.com/matzehuels/stacktower/pkg/core/deps/golang
// [metadata.GitHubProvider]: github.com/matzehuels/stacktower/pkg/core/deps/metadata.GitHubProvider
package deps
