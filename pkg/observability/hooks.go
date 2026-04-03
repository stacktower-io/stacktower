// Package observability provides hooks for metrics, tracing, and logging.
//
// This package enables optional instrumentation without adding hard dependencies
// on specific observability backends. Consumers can register hooks at startup
// to receive events about pipeline execution, cache operations, and API calls.
//
// # Architecture
//
// The package uses a simple hooks pattern:
//   - Define hook interfaces for different event categories
//   - Provide no-op default implementations
//   - Allow registration of custom implementations at startup
//
// This approach:
//   - Avoids import cycles (hooks are registered by main, not by libraries)
//   - Keeps the core library dependency-free from observability frameworks
//   - Allows different backends (OpenTelemetry, Prometheus, DataDog, etc.)
//
// # Usage
//
// Register hooks at application startup:
//
//	func main() {
//	    observability.SetPipelineHooks(&myPipelineHooks{})
//	    observability.SetCacheHooks(&myCacheHooks{})
//	    // ... run application
//	}
//
// Libraries call hooks to emit events:
//
//	observability.Pipeline().OnParseStart(ctx, language, pkg)
//	// ... do parsing ...
//	observability.Pipeline().OnParseComplete(ctx, language, pkg, nodeCount, duration, err)
package observability

import (
	"context"
	"sync"
	"time"
)

// =============================================================================
// Pipeline Hooks
// =============================================================================

// PipelineHooks receives events from the visualization pipeline.
type PipelineHooks interface {
	// Parse events
	OnParseStart(ctx context.Context, language, pkg string)
	OnParseComplete(ctx context.Context, language, pkg string, nodeCount int, duration time.Duration, err error)

	// Layout events
	OnLayoutStart(ctx context.Context, vizType string, nodeCount int)
	OnLayoutComplete(ctx context.Context, vizType string, duration time.Duration, err error)

	// Ordering events (during layout for tower visualization)
	OnOrderingStart(ctx context.Context, algorithm string, rowCount int)
	OnOrderingProgress(ctx context.Context, explored, pruned, bestCrossings int)
	OnOrderingComplete(ctx context.Context, crossings int, duration time.Duration)

	// Render events
	OnRenderStart(ctx context.Context, formats []string)
	OnRenderComplete(ctx context.Context, formats []string, duration time.Duration, err error)
}

// =============================================================================
// Cache Hooks
// =============================================================================

// CacheHooks receives events from cache operations.
type CacheHooks interface {
	// OnCacheHit records a cache hit.
	OnCacheHit(ctx context.Context, keyType string)

	// OnCacheMiss records a cache miss.
	OnCacheMiss(ctx context.Context, keyType string)

	// OnCacheSet records a cache write.
	OnCacheSet(ctx context.Context, keyType string, size int)
}

// =============================================================================
// HTTP Hooks
// =============================================================================

// HTTPHooks receives events from HTTP client operations.
type HTTPHooks interface {
	// OnRequest records an outgoing HTTP request.
	OnRequest(ctx context.Context, method, host, path string)

	// OnResponse records an HTTP response.
	OnResponse(ctx context.Context, method, host, path string, statusCode int, duration time.Duration)

	// OnError records an HTTP error (network failure, timeout).
	OnError(ctx context.Context, method, host, path string, err error)
}

// =============================================================================
// Security Hooks
// =============================================================================

// SecurityHooks receives events from security scanning operations.
type SecurityHooks interface {
	// OnScanStart records the start of a vulnerability scan.
	OnScanStart(ctx context.Context, ecosystem string, depCount int)

	// OnScanComplete records the completion of a vulnerability scan.
	OnScanComplete(ctx context.Context, ecosystem string, findingCount int, duration time.Duration, err error)
}

// =============================================================================
// Rate Limit Hooks
// =============================================================================

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	// CircuitClosed is the normal operating state where requests are allowed.
	CircuitClosed CircuitState = "closed"
	// CircuitOpen is the state where requests are rejected to allow recovery.
	CircuitOpen CircuitState = "open"
	// CircuitHalfOpen is the testing state where one probe request is allowed.
	CircuitHalfOpen CircuitState = "half-open"
)

// RateLimitHooks receives events from rate limiting operations.
type RateLimitHooks interface {
	// OnRateLimitWait records time spent waiting for rate limit tokens.
	OnRateLimitWait(ctx context.Context, registry string, waitDuration time.Duration)

	// OnRetry records a retry attempt with backoff.
	OnRetry(ctx context.Context, registry string, attempt int, backoffDuration time.Duration)

	// OnRateLimitHit records when an HTTP 429 rate limit response is received.
	OnRateLimitHit(ctx context.Context, registry string, retryAfterSeconds int)

	// OnCircuitStateChange records when a circuit breaker changes state.
	// The until parameter indicates when the circuit will transition (zero for immediate/unknown).
	OnCircuitStateChange(ctx context.Context, registry string, state CircuitState, until time.Time)
}

// =============================================================================
// Resolver Hooks
// =============================================================================

// ResolverHooks receives events from the dependency resolver's worker pool.
// Implementations must be safe for concurrent use from multiple goroutines.
type ResolverHooks interface {
	// OnFetchStart is called when a worker begins fetching a package.
	OnFetchStart(ctx context.Context, pkg string, depth int)

	// OnFetchComplete is called when a worker finishes fetching a package.
	// depCount is the number of dependencies found (0 on error).
	OnFetchComplete(ctx context.Context, pkg string, depth int, depCount int, err error)

	// OnProgress is called after each result is collected with aggregate counters.
	OnProgress(ctx context.Context, resolved, pending, maxNodes int)

	// OnEnrichStart is called when batch enrichment begins (e.g., GitHub GraphQL).
	// provider identifies the enrichment source (e.g., "github").
	// count is the number of packages being enriched.
	OnEnrichStart(ctx context.Context, provider string, count int)

	// OnEnrichComplete is called when batch enrichment finishes.
	// enriched is the number of packages successfully enriched.
	OnEnrichComplete(ctx context.Context, provider string, enriched int, err error)
}

// =============================================================================
// No-op Implementations
// =============================================================================

// NoopPipelineHooks is a no-op implementation of PipelineHooks.
type NoopPipelineHooks struct{}

func (NoopPipelineHooks) OnParseStart(context.Context, string, string) {}
func (NoopPipelineHooks) OnParseComplete(context.Context, string, string, int, time.Duration, error) {
}
func (NoopPipelineHooks) OnLayoutStart(context.Context, string, int)                       {}
func (NoopPipelineHooks) OnLayoutComplete(context.Context, string, time.Duration, error)   {}
func (NoopPipelineHooks) OnOrderingStart(context.Context, string, int)                     {}
func (NoopPipelineHooks) OnOrderingProgress(context.Context, int, int, int)                {}
func (NoopPipelineHooks) OnOrderingComplete(context.Context, int, time.Duration)           {}
func (NoopPipelineHooks) OnRenderStart(context.Context, []string)                          {}
func (NoopPipelineHooks) OnRenderComplete(context.Context, []string, time.Duration, error) {}

// NoopCacheHooks is a no-op implementation of CacheHooks.
type NoopCacheHooks struct{}

func (NoopCacheHooks) OnCacheHit(context.Context, string)      {}
func (NoopCacheHooks) OnCacheMiss(context.Context, string)     {}
func (NoopCacheHooks) OnCacheSet(context.Context, string, int) {}

// NoopHTTPHooks is a no-op implementation of HTTPHooks.
type NoopHTTPHooks struct{}

func (NoopHTTPHooks) OnRequest(context.Context, string, string, string)                      {}
func (NoopHTTPHooks) OnResponse(context.Context, string, string, string, int, time.Duration) {}
func (NoopHTTPHooks) OnError(context.Context, string, string, string, error)                 {}

// NoopSecurityHooks is a no-op implementation of SecurityHooks.
type NoopSecurityHooks struct{}

func (NoopSecurityHooks) OnScanStart(context.Context, string, int)                          {}
func (NoopSecurityHooks) OnScanComplete(context.Context, string, int, time.Duration, error) {}

// NoopRateLimitHooks is a no-op implementation of RateLimitHooks.
type NoopRateLimitHooks struct{}

func (NoopRateLimitHooks) OnRateLimitWait(context.Context, string, time.Duration)                {}
func (NoopRateLimitHooks) OnRetry(context.Context, string, int, time.Duration)                   {}
func (NoopRateLimitHooks) OnRateLimitHit(context.Context, string, int)                           {}
func (NoopRateLimitHooks) OnCircuitStateChange(context.Context, string, CircuitState, time.Time) {}

// NoopResolverHooks is a no-op implementation of ResolverHooks.
type NoopResolverHooks struct{}

func (NoopResolverHooks) OnFetchStart(context.Context, string, int)                {}
func (NoopResolverHooks) OnFetchComplete(context.Context, string, int, int, error) {}
func (NoopResolverHooks) OnProgress(context.Context, int, int, int)                {}
func (NoopResolverHooks) OnEnrichStart(context.Context, string, int)               {}
func (NoopResolverHooks) OnEnrichComplete(context.Context, string, int, error)     {}

// =============================================================================
// Global Hook Registry
// =============================================================================

var (
	pipelineHooks  PipelineHooks  = NoopPipelineHooks{}
	cacheHooks     CacheHooks     = NoopCacheHooks{}
	httpHooks      HTTPHooks      = NoopHTTPHooks{}
	securityHooks  SecurityHooks  = NoopSecurityHooks{}
	rateLimitHooks RateLimitHooks = NoopRateLimitHooks{}
	resolverHooks  ResolverHooks  = NoopResolverHooks{}
	hooksMu        sync.RWMutex
)

// SetPipelineHooks registers custom pipeline hooks.
// This should be called once at application startup before any pipeline operations.
func SetPipelineHooks(h PipelineHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		pipelineHooks = h
	}
}

// SetCacheHooks registers custom cache hooks.
// This should be called once at application startup before any cache operations.
func SetCacheHooks(h CacheHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		cacheHooks = h
	}
}

// SetHTTPHooks registers custom HTTP hooks.
// This should be called once at application startup before any HTTP operations.
func SetHTTPHooks(h HTTPHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		httpHooks = h
	}
}

// SetSecurityHooks registers custom security hooks.
// This should be called once at application startup before any security scanning.
func SetSecurityHooks(h SecurityHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		securityHooks = h
	}
}

// SetRateLimitHooks registers custom rate limit hooks.
// Unlike other hooks, this may be called per-command to install a progress view
// for the duration of an operation. Pass nil to restore the no-op default.
func SetRateLimitHooks(h RateLimitHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		rateLimitHooks = h
	} else {
		rateLimitHooks = NoopRateLimitHooks{}
	}
}

// SetResolverHooks registers custom resolver hooks.
// Unlike other hooks, this may be called per-command to install a progress view
// for the duration of a resolve operation. Pass nil to restore the no-op default.
func SetResolverHooks(h ResolverHooks) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h != nil {
		resolverHooks = h
	} else {
		resolverHooks = NoopResolverHooks{}
	}
}

// Pipeline returns the registered pipeline hooks.
func Pipeline() PipelineHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return pipelineHooks
}

// Cache returns the registered cache hooks.
func Cache() CacheHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return cacheHooks
}

// HTTP returns the registered HTTP hooks.
func HTTP() HTTPHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return httpHooks
}

// Security returns the registered security hooks.
func Security() SecurityHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return securityHooks
}

// RateLimit returns the registered rate limit hooks.
func RateLimit() RateLimitHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return rateLimitHooks
}

// Resolver returns the registered resolver hooks.
// If context-based hooks are set via WithResolverHooks, those take precedence
// over the global hooks, enabling per-request hooks for concurrent operations.
func Resolver() ResolverHooks {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	return resolverHooks
}

// =============================================================================
// Context-Based Hooks (for concurrent operations)
// =============================================================================

type contextKey int

const (
	resolverHooksKey contextKey = iota
	pipelineHooksKey
)

// WithResolverHooks returns a context with resolver hooks attached.
// Use this for per-job hooks in concurrent server environments.
// The hooks in context take precedence over global hooks.
func WithResolverHooks(ctx context.Context, hooks ResolverHooks) context.Context {
	return context.WithValue(ctx, resolverHooksKey, hooks)
}

// WithPipelineHooks returns a context with pipeline hooks attached.
// Use this for per-job hooks in concurrent server environments.
func WithPipelineHooks(ctx context.Context, hooks PipelineHooks) context.Context {
	return context.WithValue(ctx, pipelineHooksKey, hooks)
}

// ResolverFromContext returns resolver hooks from context if set,
// otherwise returns the global resolver hooks.
func ResolverFromContext(ctx context.Context) ResolverHooks {
	if hooks, ok := ctx.Value(resolverHooksKey).(ResolverHooks); ok && hooks != nil {
		return hooks
	}
	return Resolver()
}

// PipelineFromContext returns pipeline hooks from context if set,
// otherwise returns the global pipeline hooks.
func PipelineFromContext(ctx context.Context) PipelineHooks {
	if hooks, ok := ctx.Value(pipelineHooksKey).(PipelineHooks); ok && hooks != nil {
		return hooks
	}
	return Pipeline()
}

// Reset restores all hooks to their no-op defaults.
// This is primarily useful for testing.
func Reset() {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	pipelineHooks = NoopPipelineHooks{}
	cacheHooks = NoopCacheHooks{}
	httpHooks = NoopHTTPHooks{}
	securityHooks = NoopSecurityHooks{}
	rateLimitHooks = NoopRateLimitHooks{}
	resolverHooks = NoopResolverHooks{}
}
