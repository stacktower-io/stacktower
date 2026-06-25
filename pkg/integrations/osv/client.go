package osv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/integrations"
)

const (
	// DefaultBaseURL is the OSV.dev API endpoint.
	DefaultBaseURL = "https://api.osv.dev"

	// MaxBatchSize is the maximum number of queries per batch request.
	// OSV.dev supports up to 1000 queries per batch.
	MaxBatchSize = 1000

	// maxConcurrentBatches caps how many sub-batch requests run in parallel
	// when a query set exceeds MaxBatchSize.
	maxConcurrentBatches = 4

	// DefaultCacheTTL is how long vulnerability data is cached.
	// Vulnerability data changes less frequently than package metadata.
	DefaultCacheTTL = 6 * time.Hour
)

// Client queries the OSV.dev vulnerability database.
//
// It is built on the shared [integrations.Client], which provides response
// caching, retry on transient failures, request deduplication, proactive
// rate limiting, circuit breaking, and response size limits.
//
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	client  *integrations.Client
	baseURL string
}

// NewClient creates an OSV.dev client with optional caching.
//
// Parameters:
//   - backend: Cache backend for response caching. If nil, no caching is performed.
//   - cacheTTL: How long to cache responses. If <= 0, uses DefaultCacheTTL.
//
// Rate limits are configured via integrations.DefaultRateLimits["osv"].
func NewClient(backend cache.Cache, cacheTTL time.Duration) *Client {
	if cacheTTL <= 0 {
		cacheTTL = DefaultCacheTTL
	}

	rl := integrations.DefaultRateLimits["osv"]
	return &Client{
		client:  integrations.NewClientWithRateLimit(backend, "osv:", cacheTTL, nil, rl.RequestsPerSecond, rl.Burst),
		baseURL: DefaultBaseURL,
	}
}

// QueryBatch queries OSV.dev for vulnerabilities affecting the given packages.
// If len(queries) exceeds [MaxBatchSize], the request is automatically split
// into multiple batches (at most maxConcurrentBatches in flight at once) and
// results are merged.
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

	cacheKey := "batch:" + batchCacheKey(queries)

	var results []QueryResult
	doFetch := func() error {
		fetched, err := c.queryBatches(ctx, queries)
		if err != nil {
			return err
		}
		results = fetched
		return nil
	}
	if err := c.client.Cached(ctx, cacheKey, refresh, &results, doFetch); err != nil {
		return nil, err
	}
	if len(results) != len(queries) {
		// Stale or malformed cache entry; refetch with refresh=true so the
		// corrected result overwrites the bad entry instead of every future
		// call repeating this repair.
		results = nil
		if err := c.client.Cached(ctx, cacheKey, true, &results, doFetch); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// queryBatches splits queries into MaxBatchSize chunks and fetches them with
// bounded concurrency, preserving input order in the merged result.
func (c *Client) queryBatches(ctx context.Context, queries []Query) ([]QueryResult, error) {
	type batchSlice struct {
		index int // batch ordinal for ordered reassembly
		start int
		end   int
	}

	var batches []batchSlice
	for i := 0; i < len(queries); i += MaxBatchSize {
		end := min(i+MaxBatchSize, len(queries))
		batches = append(batches, batchSlice{index: len(batches), start: i, end: end})
	}

	batchResults := make([][]QueryResult, len(batches))

	if len(batches) == 1 {
		results, err := c.queryBatchSingle(ctx, queries)
		if err != nil {
			return nil, err
		}
		batchResults[0] = results
	} else {
		var (
			mu       sync.Mutex
			wg       sync.WaitGroup
			batchErr error
		)
		sem := make(chan struct{}, maxConcurrentBatches)
		for _, b := range batches {
			wg.Add(1)
			go func(b batchSlice) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

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

	var vuln Vulnerability
	endpoint := c.baseURL + "/v1/vulns/" + url.PathEscape(id)
	err := c.client.Cached(ctx, "vuln:"+id, refresh, &vuln, func() error {
		return c.client.Get(ctx, endpoint, &vuln)
	})
	if err != nil {
		return nil, err
	}
	return &vuln, nil
}

// batchCacheKey generates a deterministic cache key for a batch of queries.
func batchCacheKey(queries []Query) string {
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
	var batchResp BatchResponse
	if err := c.client.PostJSON(ctx, c.baseURL+"/v1/querybatch", BatchRequest{Queries: queries}, &batchResp); err != nil {
		return nil, fmt.Errorf("osv querybatch: %w", err)
	}
	return batchResp.Results, nil
}
