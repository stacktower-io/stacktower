// Package osv provides a client for the OSV.dev vulnerability database API.
//
// OSV.dev (https://osv.dev) is a free, open vulnerability database that covers
// all major package ecosystems: npm, PyPI, Go, crates.io, Maven, RubyGems,
// and Packagist.
//
// # API
//
// This client uses the batch query endpoint for efficient scanning:
//
//	POST https://api.osv.dev/v1/querybatch
//
// A single batch request can check hundreds of packages at once, making it
// suitable for scanning entire dependency trees.
//
// # Usage
//
//	client := osv.NewClient(nil)  // nil = default HTTP client
//	resp, err := client.QueryBatch(ctx, []osv.Query{
//	    {Package: osv.PackageQuery{Name: "requests", Ecosystem: "PyPI"}, Version: "2.28.0"},
//	})
//
// # Rate Limits
//
// OSV.dev has generous rate limits for the public API. No authentication is required.
// The client does not implement its own rate limiting; callers should handle
// [integrations.RateLimitedError] if needed.
package osv
