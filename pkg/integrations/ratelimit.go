package integrations

// BurstLimit defines burst limiting parameters for a registry client.
// These limits prevent request stampedes when the resolver fires many concurrent
// requests. They are NOT intended to stay under registry rate limits - that's
// handled by the retry/backoff mechanism and circuit breaker responding to 429s.
//
// The goal is to prevent flooding a registry with 100+ simultaneous requests
// before we even receive the first 429 response.
type BurstLimit struct {
	// RequestsPerSecond is the sustained token refill rate.
	// Set high enough to not slow down normal operation.
	RequestsPerSecond float64
	// Burst is the maximum concurrent requests allowed in a burst.
	// This is the key constraint - limits how many requests can fire at once.
	Burst int
}

// RateLimit is an alias for BurstLimit for backward compatibility.
type RateLimit = BurstLimit

// DefaultRateLimits provides per-registry burst limits.
//
// These are intentionally generous - they exist to prevent stampedes, not to
// enforce registry rate limits. Actual rate limit compliance is handled by:
//   - Retry with backoff (honors Retry-After header)
//   - Circuit breaker (opens after consecutive 429s)
//
// The Burst value is the key constraint: it limits how many requests can
// be in-flight before we start getting 429 responses back.
//
// Registry-specific notes:
//   - crates.io: Has strict 1 req/s policy, so we use lower burst
//   - GitHub: Has hard hourly limits, so we use lower values
//   - Others: CDN-backed or generous, higher bursts are safe
var DefaultRateLimits = map[string]BurstLimit{
	"pypi":          {RequestsPerSecond: 50, Burst: 30},
	"npm":           {RequestsPerSecond: 50, Burst: 30},
	"crates":        {RequestsPerSecond: 5, Burst: 10}, // strict 1/s policy
	"rubygems":      {RequestsPerSecond: 30, Burst: 20},
	"packagist":     {RequestsPerSecond: 30, Burst: 20},
	"maven":         {RequestsPerSecond: 30, Burst: 20},
	"goproxy":       {RequestsPerSecond: 50, Burst: 30},   // CDN-backed
	"github":        {RequestsPerSecond: 10, Burst: 50},   // 5000/hour limit, higher burst for parallel enrichment
	"github_unauth": {RequestsPerSecond: 0.015, Burst: 5}, // 60/hour limit
	"osv":           {RequestsPerSecond: 20, Burst: 15},
}
