// Package pypi provides an HTTP client for the Python Package Index API.
//
// # Overview
//
// This package fetches package metadata from PyPI (https://pypi.org), the
// official repository for Python packages.
//
// # Usage
//
//	client := pypi.NewClient(backend, 24*time.Hour, "3.12")
//
//	pkg, err := client.FetchPackage(ctx, "fastapi", false) // false = use cache
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println(pkg.Name, pkg.Version)
//	fmt.Println("Dependencies:", pkg.Dependencies)
//
// # PackageInfo
//
// [FetchPackage] returns a [PackageInfo] containing:
//
//   - Name, Version: Package identity
//   - Dependencies: Direct runtime dependencies (extras/dev filtered out)
//   - Summary: Package description
//   - License, Author: Package metadata
//   - ProjectURLs, HomePage: Links for enrichment
//
// # Caching
//
// Responses are cached to reduce load on PyPI and speed up repeated requests.
// The cache TTL is set when creating the client. Pass refresh=true to
// [FetchPackage] to bypass the cache.
//
// # Dependency Filtering
//
// Dependencies are extracted from requires_dist, filtering out:
//
//   - Optional extras (extra markers)
//   - Development dependencies (dev markers)
//   - Test dependencies (test markers)
//
// Package names are normalized following PEP 503.
package pypi
