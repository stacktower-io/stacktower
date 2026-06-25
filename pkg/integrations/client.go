package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/observability"
)

// MaxResponseSize is the maximum allowed HTTP response body size (25MB).
// Responses larger than this are rejected to prevent memory exhaustion.
// Set to 25MB to accommodate large PyPI project metadata (e.g., pydantic-core
// has >10MB of release file metadata across all platforms/versions).
const MaxResponseSize = 25 * 1024 * 1024

// Client provides shared HTTP functionality for all registry API clients.
// It handles caching, retry logic, request deduplication, proactive rate limiting,
// circuit breaking, and common request headers.
//
// Client is safe for concurrent use by multiple goroutines.
// The underlying HTTP client, cache, and headers are all goroutine-safe.
//
// Zero values: Do not use an uninitialized Client; always create via [NewClient].
type Client struct {
	http           *http.Client
	cache          cache.Cache
	keyer          cache.Keyer
	namespace      string        // Cache key prefix (e.g., "pypi:", "npm:")
	ttl            time.Duration // Cache TTL
	headers        map[string]string
	group          singleflight.Group // deduplicates concurrent in-flight requests
	limiter        *rate.Limiter      // proactive token-bucket rate limiter (nil = no limit)
	circuitBreaker *CircuitBreaker    // circuit breaker for rate limit protection
}

// Shared per-registry infrastructure.
//
// Multiple Client instances are routinely created for the same registry within
// one process (the resolver's fetcher, URL providers, runtime probes, ...).
// If each instance carried its own rate limiter and circuit breaker, the
// effective request budget would be N times the configured limit and a 429
// storm observed by one client would not protect the others. Known registries
// (those listed in [DefaultRateLimits]) therefore share a single limiter and
// circuit breaker per registry. Unknown namespaces (e.g. tests) get private
// instances to preserve isolation.
var (
	sharedInfraMu  sync.Mutex
	sharedBreakers = map[string]*CircuitBreaker{}
	sharedLimiters = map[string]*rate.Limiter{}
)

// normalizeRegistry maps composite namespace identifiers (e.g. "github:auth:<hash>",
// "github:unauth") back to the canonical registry key used in [DefaultRateLimits].
// This ensures that all GitHub client instances share the correct limiter and
// circuit breaker regardless of token-scoped cache namespaces.
func normalizeRegistry(registry string) string {
	if _, ok := DefaultRateLimits[registry]; ok {
		return registry
	}
	// Handle composite keys like "github:auth:<hash>" or "github:unauth".
	if prefix, rest, found := strings.Cut(registry, ":"); found {
		// Try prefix_suffix first ("github:unauth" → "github_unauth") to
		// match budget-specific entries before falling back to the prefix alone.
		suffix, _, _ := strings.Cut(rest, ":")
		if suffix != "" {
			candidate := prefix + "_" + suffix
			if _, ok := DefaultRateLimits[candidate]; ok {
				return candidate
			}
		}
		// Fall back to prefix alone ("github:auth:<hash>" → "github").
		if _, ok := DefaultRateLimits[prefix]; ok {
			return prefix
		}
	}
	return registry
}

// isKnownRegistry reports whether the registry has production rate limit
// configuration and should share limiter/breaker state across clients.
func isKnownRegistry(registry string) bool {
	_, ok := DefaultRateLimits[normalizeRegistry(registry)]
	return ok
}

func circuitBreakerForRegistry(registry string) *CircuitBreaker {
	normalized := normalizeRegistry(registry)
	if !isKnownRegistry(normalized) {
		return NewCircuitBreaker(registry, DefaultCircuitBreakerConfig())
	}
	sharedInfraMu.Lock()
	defer sharedInfraMu.Unlock()
	if cb, ok := sharedBreakers[normalized]; ok {
		return cb
	}
	cb := NewCircuitBreaker(normalized, DefaultCircuitBreakerConfig())
	sharedBreakers[normalized] = cb
	return cb
}

// limiterForRegistry returns the shared limiter for a known registry,
// creating it on first use. The key includes rps/burst so that distinct
// budgets for the same registry (e.g. authenticated vs unauthenticated
// GitHub) get separate limiters.
func limiterForRegistry(registry string, rps float64, burst int) *rate.Limiter {
	normalized := normalizeRegistry(registry)
	if !isKnownRegistry(normalized) {
		return rate.NewLimiter(rate.Limit(rps), burst)
	}
	key := fmt.Sprintf("%s|%g|%d", normalized, rps, burst)
	sharedInfraMu.Lock()
	defer sharedInfraMu.Unlock()
	if l, ok := sharedLimiters[key]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(rps), burst)
	sharedLimiters[key] = l
	return l
}

// NewClient creates a Client with the given cache and default headers.
// Headers are applied to all requests made through this client.
//
// The HTTP timeout is automatically configured based on the registry namespace
// using [DefaultTimeouts]. For example, "pypi:" uses 10s, "maven:" uses 30s.
//
// Parameters:
//   - c: Cache for caching HTTP responses. If nil, a NullCache is used (no caching).
//   - namespace: Cache key prefix for this client (e.g., "pypi:", "npm:").
//   - ttl: How long to cache responses.
//   - headers: Default HTTP headers for all requests. Pass nil if no default headers
//     are needed. Common examples: "Authorization", "User-Agent", "Accept".
//
// Clients for known registries (see [DefaultRateLimits]) share one circuit
// breaker per registry, so rate-limit protection applies across all client
// instances in the process.
//
// The returned Client is safe for concurrent use by multiple goroutines.
func NewClient(c cache.Cache, namespace string, ttl time.Duration, headers map[string]string) *Client {
	if c == nil {
		c = cache.NewNullCache()
	}
	registry := strings.TrimSuffix(namespace, ":")
	normalized := normalizeRegistry(registry)
	return &Client{
		http:           NewHTTPClientWithTimeout(TimeoutForRegistry(normalized)),
		cache:          c,
		keyer:          cache.NewDefaultKeyer(),
		namespace:      namespace,
		ttl:            ttl,
		headers:        headers,
		circuitBreaker: circuitBreakerForRegistry(registry),
	}
}

// NewClientWithRateLimit creates a Client with proactive rate limiting.
// The limiter throttles outbound requests to stay within the registry's rate limits,
// preventing 429 errors proactively rather than only reacting to them.
//
// Clients for known registries (see [DefaultRateLimits]) share one limiter per
// registry+budget, so the configured rate applies across all client instances
// in the process rather than multiplying per instance.
//
// Parameters are the same as [NewClient], plus:
//   - rps: Maximum sustained requests per second. If <= 0, no rate limiting is applied.
//   - burst: Maximum burst size (concurrent requests allowed at once). If <= 0, defaults to 1.
func NewClientWithRateLimit(c cache.Cache, namespace string, ttl time.Duration, headers map[string]string, rps float64, burst int) *Client {
	client := NewClient(c, namespace, ttl, headers)
	if rps > 0 {
		if burst <= 0 {
			burst = 1
		}
		client.limiter = limiterForRegistry(client.registryName(), rps, burst)
	}
	return client
}

// registryName returns the registry identifier from the namespace (e.g., "pypi:" -> "pypi").
func (c *Client) registryName() string {
	return strings.TrimSuffix(c.namespace, ":")
}

// Cached retrieves a value from cache or executes fetch and caches the result.
// If refresh is true, the cache is bypassed and fetch is always called.
//
// Concurrent requests for the same key are deduplicated via singleflight:
// only one fetch executes and all callers receive the same result.
//
// Parameters:
//   - ctx: Context for cancellation. If cancelled, fetch is not executed and returns ctx.Err().
//   - key: Cache key (usually package name or coordinate). Must not be empty.
//   - refresh: If true, bypass cache and always call fetch. If false, try cache first.
//   - v: Pointer to store the result. Must be a non-nil pointer to a JSON-serializable type.
//   - fetch: Function to fetch data and populate v. Called with retry on transient failures.
//
// Behavior:
//  1. If refresh=false and cache hit: returns nil immediately with v populated
//  2. If cache miss or refresh=true: calls fetch with automatic retry on [RetryableError]
//  3. Concurrent fetches for the same key are deduplicated (only one HTTP call)
//  4. On successful fetch: stores result in cache (ignoring cache write errors)
//
// The fetch function should populate v and return nil on success, or return an error.
// Network errors should be wrapped with [Retryable] to enable retry.
//
// Observability hooks are emitted for cache hits, misses, and writes via [observability.Cache].
//
// Returns:
//   - nil on success (v is populated)
//   - error from fetch if it fails (v may be partially populated)
//   - ctx.Err() if context is cancelled
//
// This method is safe for concurrent use on the same Client.
func (c *Client) Cached(ctx context.Context, key string, refresh bool, v any, fetch func() error) error {
	cacheKey := c.keyer.HTTPKey(c.namespace, key)
	registry := c.registryName()

	if !refresh {
		data, hit, err := c.cache.Get(ctx, cacheKey)
		if err != nil {
			slog.Debug("cache get failed, falling back to fetch", "key", key, "error", err)
		}
		if hit {
			if err := json.Unmarshal(data, v); err == nil {
				observability.Cache().OnCacheHit(ctx, registry)
				return nil
			}
		}
		observability.Cache().OnCacheMiss(ctx, registry)
	}

	// Singleflight: deduplicate concurrent fetches for the same cache key.
	// Only one goroutine executes fetch; others wait and receive the shared result.
	// The cache write happens once inside the winning call rather than in every
	// waiter, avoiding N duplicate cache writes for N concurrent callers.
	result, err, _ := c.group.Do(cacheKey, func() (any, error) {
		if err := cache.RetryWithBackoffRegistry(ctx, registry, fetch); err != nil {
			return nil, err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal cached value: %w", err)
		}
		if err := c.cache.Set(ctx, cacheKey, data, c.ttl); err == nil {
			observability.Cache().OnCacheSet(ctx, registry, len(data))
		}
		return data, nil
	})
	if err != nil {
		return err
	}

	// Populate v from the shared result
	if data, ok := result.([]byte); ok && data != nil {
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("unmarshal cached result for %s/%s: %w", registry, key, err)
		}
	}
	return nil
}

// Get performs an HTTP GET request and JSON-decodes the response into v.
// It uses the client's default headers and handles retries automatically.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - url: Full URL to request (must be absolute URL with scheme)
//   - v: Pointer to store decoded JSON response (must be non-nil)
//
// Returns:
//   - [ErrNotFound] for HTTP 404 responses
//   - [ErrNetwork] wrapped with [RetryableError] for HTTP 5xx responses
//   - [ErrNetwork] for connection failures and timeouts
//   - json decoding errors if response is not valid JSON
//
// This method is safe for concurrent use on the same Client.
func (c *Client) Get(ctx context.Context, url string, v any) error {
	return c.GetWithHeaders(ctx, url, nil, v)
}

// GetWithHeaders performs an HTTP GET with additional headers merged with defaults.
// Request-specific headers override client defaults for the same key.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - url: Full URL to request (must be absolute URL with scheme)
//   - headers: Additional headers for this request only (may be nil). Headers with the
//     same key as client defaults will override the default value for this request.
//   - v: Pointer to store decoded JSON response (must be non-nil)
//
// Example:
//
//	err := client.GetWithHeaders(ctx, url, map[string]string{"X-Custom": "value"}, &resp)
//
// Returns the same errors as [Get].
// This method is safe for concurrent use on the same Client.
func (c *Client) GetWithHeaders(ctx context.Context, url string, headers map[string]string, v any) error {
	body, err := c.doRequest(ctx, url, headers)
	if err != nil {
		return err
	}
	defer body.Close()

	// Limit response size to prevent memory exhaustion from large/malicious responses
	limited := &io.LimitedReader{R: body, N: MaxResponseSize + 1}
	if err := json.NewDecoder(limited).Decode(v); err != nil {
		if limited.N <= 0 {
			return fmt.Errorf("response exceeds maximum size of %d bytes", MaxResponseSize)
		}
		// EOF-family errors during body read indicate a truncated response
		// (connection dropped, server closed early). Treat as retryable network error.
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return cache.Retryable(fmt.Errorf("%w: %s: %v", ErrNetwork, url, err))
		}
		return fmt.Errorf("decode response from %s: %w", url, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("response exceeds maximum size of %d bytes", MaxResponseSize)
	}
	return nil
}

// GetText performs an HTTP GET request and returns the response body as a string.
// Useful for non-JSON endpoints like go.mod files or plain text responses.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - url: Full URL to request (must be absolute URL with scheme)
//
// Response size is limited to [MaxResponseSize] bytes to prevent memory exhaustion.
//
// Returns:
//   - The response body as a string
//   - [ErrNotFound] for HTTP 404 responses
//   - [ErrNetwork] for connection failures, timeouts, and HTTP 5xx responses
//   - Error if response exceeds [MaxResponseSize]
//   - io errors if reading the response body fails
//
// This method is safe for concurrent use on the same Client.
func (c *Client) GetText(ctx context.Context, url string) (string, error) {
	body, err := c.doRequest(ctx, url, nil)
	if err != nil {
		return "", err
	}
	defer body.Close()

	// Limit response size to prevent memory exhaustion
	limited := io.LimitReader(body, MaxResponseSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return "", cache.Retryable(fmt.Errorf("%w: %s: %v", ErrNetwork, url, err))
		}
		return "", err
	}
	if len(data) > MaxResponseSize {
		return "", fmt.Errorf("response exceeds maximum size of %d bytes", MaxResponseSize)
	}
	return string(data), nil
}

// PostJSON sends a POST request with a JSON body and decodes the response.
// Uses the client's default headers and rate limiter.
// Response size is limited to [MaxResponseSize] bytes.
func (c *Client) PostJSON(ctx context.Context, url string, body any, v any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}
	respBody, err := c.doRequestWithBody(ctx, http.MethodPost, url, data, nil)
	if err != nil {
		return err
	}
	defer respBody.Close()

	// Limit response size to prevent memory exhaustion
	limited := &io.LimitedReader{R: respBody, N: MaxResponseSize + 1}
	if err := json.NewDecoder(limited).Decode(v); err != nil {
		if limited.N <= 0 {
			return fmt.Errorf("response exceeds maximum size of %d bytes", MaxResponseSize)
		}
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return cache.Retryable(fmt.Errorf("%w: %s: %v", ErrNetwork, url, err))
		}
		return fmt.Errorf("decode response from %s: %w", url, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("response exceeds maximum size of %d bytes", MaxResponseSize)
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, url string, headers map[string]string) (io.ReadCloser, error) {
	return c.doRequestWithBody(ctx, http.MethodGet, url, nil, headers)
}

func (c *Client) doRequestWithBody(ctx context.Context, method, reqURL string, body []byte, headers map[string]string) (io.ReadCloser, error) {
	if c.circuitBreaker != nil && !c.circuitBreaker.Allow(ctx) {
		return nil, fmt.Errorf("%s %s: %w", method, reqURL, ErrCircuitOpen)
	}

	if c.limiter != nil {
		r := c.limiter.Reserve()
		if !r.OK() {
			return nil, fmt.Errorf("rate limit: would exceed burst")
		}
		delay := r.Delay()
		if delay > time.Millisecond {
			observability.RateLimit().OnRateLimitWait(ctx, c.registryName(), delay)
			// time.NewTimer + Stop instead of time.After: After leaks its
			// timer until expiry when ctx wins the select, which adds up
			// under heavy rate limiting.
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				r.Cancel()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	host := req.URL.Host
	path := req.URL.Path
	observability.HTTP().OnRequest(ctx, method, host, path)
	start := time.Now()

	resp, err := c.http.Do(req)
	if err != nil {
		observability.HTTP().OnError(ctx, method, host, path, err)
		return nil, cache.Retryable(fmt.Errorf("%w: %s %s: %v", ErrNetwork, method, reqURL, err))
	}

	observability.HTTP().OnResponse(ctx, method, host, path, resp.StatusCode, time.Since(start))

	if err := checkResponse(resp); err != nil {
		resp.Body.Close()
		if IsRateLimitedError(err) {
			var rle *RateLimitedError
			if errors.As(err, &rle) {
				observability.RateLimit().OnRateLimitHit(ctx, c.registryName(), rle.RetryAfter)
				if c.circuitBreaker != nil {
					c.circuitBreaker.RecordFailure(ctx, rle.RetryAfter)
				}
			}
		}
		return nil, fmt.Errorf("%s %s: %w", method, reqURL, err)
	}

	if c.circuitBreaker != nil {
		c.circuitBreaker.RecordSuccess(ctx)
	}
	return resp.Body, nil
}

func checkResponse(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusForbidden:
		// GitHub (and some other APIs) signal rate limiting with 403 rather
		// than 429. Distinguish via headers so callers retry with backoff
		// instead of failing hard with a non-retryable auth error.
		if isRateLimited403(resp) {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			if retryAfter == 0 {
				retryAfter = retryAfterFromRateLimitReset(resp.Header.Get("X-RateLimit-Reset"))
			}
			return cache.Retryable(&RateLimitedError{RetryAfter: retryAfter})
		}
		return ErrUnauthorized
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return cache.Retryable(&RateLimitedError{RetryAfter: retryAfter})
	case resp.StatusCode >= 500:
		return cache.Retryable(fmt.Errorf("%w: status %d", ErrNetwork, resp.StatusCode))
	default:
		return fmt.Errorf("%w: status %d", ErrNetwork, resp.StatusCode)
	}
}

// isRateLimited403 reports whether a 403 response is actually a rate-limit
// signal. GitHub sets X-RateLimit-Remaining: 0 when the primary quota is
// exhausted and Retry-After for secondary (abuse) limits.
func isRateLimited403(resp *http.Response) bool {
	if resp.Header.Get("Retry-After") != "" {
		return true
	}
	return resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// retryAfterFromRateLimitReset converts GitHub's X-RateLimit-Reset header
// (Unix epoch seconds) to a relative wait in seconds.
func retryAfterFromRateLimitReset(value string) int {
	if value == "" {
		return 0
	}
	epoch, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	if delay := int(time.Until(time.Unix(epoch, 0)).Seconds()); delay > 0 {
		return delay
	}
	return 0
}

// parseRetryAfter parses the Retry-After header value, which may be either
// a number of seconds or an HTTP-date (RFC 7231).
func parseRetryAfter(value string) int {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return seconds
	}
	if t, err := http.ParseTime(value); err == nil {
		delay := int(time.Until(t).Seconds())
		if delay > 0 {
			return delay
		}
	}
	return 0
}

// RateLimitedError indicates the API rate limit has been exceeded.
type RateLimitedError struct {
	RetryAfter int // Seconds to wait before retrying (0 if unknown)
}

// Error implements the error interface.
func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited: retry after %d seconds", e.RetryAfter)
	}
	return "rate limited: too many requests"
}

// RetryAfterSeconds returns the requested wait time in seconds.
func (e *RateLimitedError) RetryAfterSeconds() int {
	return e.RetryAfter
}

// IsRateLimitedError checks if an error is or wraps a [RateLimitedError].
func IsRateLimitedError(err error) bool {
	var rle *RateLimitedError
	return errors.As(err, &rle)
}
