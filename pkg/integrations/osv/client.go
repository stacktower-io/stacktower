package osv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/integrations"
)

const (
	// DefaultBaseURL is the OSV.dev API endpoint.
	DefaultBaseURL = "https://api.osv.dev"

	// DefaultTimeout is the HTTP request timeout for OSV API calls.
	// Batch queries can take longer than single-package lookups.
	DefaultTimeout = 30 * time.Second

	// MaxBatchSize is the maximum number of queries per batch request.
	// OSV.dev supports up to 1000 queries per batch.
	MaxBatchSize = 1000

	// DefaultCacheTTL is how long vulnerability data is cached.
	// Vulnerability data changes less frequently than package metadata.
	DefaultCacheTTL = 6 * time.Hour
)

// Client queries the OSV.dev vulnerability database.
// It supports response caching and proactive rate limiting.
//
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	http    *http.Client
	baseURL string
	cache   cache.Cache
	keyer   cache.Keyer
	ttl     time.Duration
	limiter *rate.Limiter
}

// NewClient creates an OSV.dev client with optional caching.
//
// Parameters:
//   - backend: Cache backend for response caching. If nil, no caching is performed.
//   - cacheTTL: How long to cache responses. If <= 0, uses DefaultCacheTTL.
//
// Rate limits are configured via integrations.DefaultRateLimits["osv"].
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	if backend == nil {
		backend = cache.NewNullCache()
	}
	if cacheTTL <= 0 {
		cacheTTL = DefaultCacheTTL
	}

	rl := integrations.DefaultRateLimits["osv"]
	return &Client{
		http:    integrations.NewHTTPClient(),
		baseURL: DefaultBaseURL,
		cache:   backend,
		keyer:   cache.NewDefaultKeyer(),
		ttl:     cacheTTL,
		limiter: rate.NewLimiter(rate.Limit(rl.RequestsPerSecond), rl.Burst),
	}
}

// QueryBatch queries OSV.dev for vulnerabilities affecting the given packages.
// If len(queries) exceeds [MaxBatchSize], the request is automatically split
// into multiple batches and results are merged.
//
// Results are cached based on the query contents. Set refresh=true to bypass cache.
//
// Returns a slice of [QueryResult] in the same order as the input queries.
// Each result contains the vulnerabilities found for that query (may be empty).
//
// Returns an error for network failures or non-200 API responses.
func (c *Client) QueryBatch(ctx context.Context, queries []Query, refresh bool) ([]QueryResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	// Generate cache key from queries
	cacheKey := c.keyer.HTTPKey("osv:", c.batchCacheKey(queries))

	// Try cache first
	if !refresh {
		var cached []QueryResult
		data, hit, _ := c.cache.Get(ctx, cacheKey)
		if hit {
			if err := json.Unmarshal(data, &cached); err == nil && len(cached) == len(queries) {
				return cached, nil
			}
		}
	}

	// Split into batches and query concurrently.
	type batchSlice struct {
		index int // batch ordinal for ordered reassembly
		start int
		end   int
	}

	var batches []batchSlice
	for i := 0; i < len(queries); i += MaxBatchSize {
		end := i + MaxBatchSize
		if end > len(queries) {
			end = len(queries)
		}
		batches = append(batches, batchSlice{index: len(batches), start: i, end: end})
	}

	batchResults := make([][]QueryResult, len(batches))
	var batchErr error

	if len(batches) == 1 {
		results, err := c.queryBatchSingle(ctx, queries[batches[0].start:batches[0].end])
		if err != nil {
			return nil, err
		}
		batchResults[0] = results
	} else {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, b := range batches {
			wg.Add(1)
			go func(b batchSlice) {
				defer wg.Done()
				results, err := c.queryBatchSingle(ctx, queries[b.start:b.end])
				mu.Lock()
				defer mu.Unlock()
				if err != nil && batchErr == nil {
					batchErr = fmt.Errorf("batch %d: %w", b.index, err)
				} else if err == nil {
					batchResults[b.index] = results
				}
			}(b)
		}
		wg.Wait()
		if batchErr != nil {
			return nil, batchErr
		}
	}

	allResults := make([]QueryResult, 0, len(queries))
	for _, br := range batchResults {
		allResults = append(allResults, br...)
	}

	// Cache the results
	if data, err := json.Marshal(allResults); err == nil {
		_ = c.cache.Set(ctx, cacheKey, data, c.ttl)
	}

	return allResults, nil
}

// GetVulnerability fetches full vulnerability details for a single OSV ID.
//
// Unlike querybatch, this endpoint usually includes rich fields such as
// summary/details/references and complete affected ranges.
func (c *Client) GetVulnerability(ctx context.Context, id string, refresh bool) (*Vulnerability, error) {
	if id == "" {
		return nil, fmt.Errorf("vulnerability id is required")
	}

	cacheKey := c.keyer.HTTPKey("osv:vuln:", id)
	if !refresh {
		var cached Vulnerability
		data, hit, _ := c.cache.Get(ctx, cacheKey)
		if hit {
			if err := json.Unmarshal(data, &cached); err == nil {
				return &cached, nil
			}
		}
	}

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}
	}

	endpoint := c.baseURL + "/v1/vulns/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", integrations.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("osv API error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var vuln Vulnerability
	if err := json.NewDecoder(resp.Body).Decode(&vuln); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if data, err := json.Marshal(vuln); err == nil {
		_ = c.cache.Set(ctx, cacheKey, data, c.ttl)
	}

	return &vuln, nil
}

// batchCacheKey generates a deterministic cache key for a batch of queries.
func (c *Client) batchCacheKey(queries []Query) string {
	// Sort queries to ensure deterministic key regardless of input order
	sorted := make([]Query, len(queries))
	copy(sorted, queries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Package.Ecosystem != sorted[j].Package.Ecosystem {
			return sorted[i].Package.Ecosystem < sorted[j].Package.Ecosystem
		}
		if sorted[i].Package.Name != sorted[j].Package.Name {
			return sorted[i].Package.Name < sorted[j].Package.Name
		}
		return sorted[i].Version < sorted[j].Version
	})

	// Hash the sorted queries
	data, _ := json.Marshal(sorted)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes for shorter key
}

func (c *Client) queryBatchSingle(ctx context.Context, queries []Query) ([]QueryResult, error) {
	// Apply rate limiting
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}
	}

	reqBody := BatchRequest{Queries: queries}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/v1/querybatch"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", integrations.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("osv request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("osv API error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var batchResp BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return batchResp.Results, nil
}
