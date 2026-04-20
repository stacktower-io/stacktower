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
// The returned Client is safe for concurrent use by multiple goroutines.
func NewClient(c cache.Cache, namespace string, ttl time.Duration, headers map[string]string) *Client {
	if c == nil {
		c = cache.NewNullCache()
	}
	registry := strings.TrimSuffix(namespace, ":")
	return &Client{
		http:           NewHTTPClientWithTimeout(TimeoutForRegistry(registry)),
		cache:          c,
		keyer:          cache.NewDefaultKeyer(),
		namespace:      namespace,
		ttl:            ttl,
		headers:        headers,
		circuitBreaker: NewCircuitBreaker(registry, DefaultCircuitBreakerConfig()),
	}
}

// NewClientWithRateLimit creates a Client with proactive rate limiting.
// The limiter throttles outbound requests to stay within the registry's rate limits,
// preventing 429 errors proactively rather than only reacting to them.
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
		client.limiter = rate.NewLimiter(rate.Limit(rps), burst)
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
	result, err, _ := c.group.Do(cacheKey, func() (any, error) {
		if err := cache.RetryWithBackoffRegistry(ctx, registry, fetch); err != nil {
			return nil, err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal cached value: %w", err)
		}
		return data, nil
	})
	if err != nil {
		return err
	}

	// Populate v from the shared result and store in cache
	if data, ok := result.([]byte); ok && data != nil {
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("unmarshal cached result for %s/%s: %w", registry, key, err)
		}
		if err := c.cache.Set(ctx, cacheKey, data, c.ttl); err == nil {
			observability.Cache().OnCacheSet(ctx, registry, len(data))
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
			select {
			case <-ctx.Done():
				r.Cancel()
				return nil, ctx.Err()
			case <-time.After(delay):
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
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		retryAfter := 0
		if v := resp.Header.Get("Retry-After"); v != "" {
			if seconds, err := strconv.Atoi(v); err == nil {
				retryAfter = seconds
			}
		}
		return cache.Retryable(&RateLimitedError{RetryAfter: retryAfter})
	case resp.StatusCode >= 500:
		return cache.Retryable(fmt.Errorf("%w: status %d", ErrNetwork, resp.StatusCode))
	default:
		return fmt.Errorf("%w: status %d", ErrNetwork, resp.StatusCode)
	}
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
