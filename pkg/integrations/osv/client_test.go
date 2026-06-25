package osv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	backend, err := cache.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	c := NewClient(backend, time.Minute)
	c.baseURL = srv.URL
	return c, srv
}

func TestQueryBatch_RoundTripAndCache(t *testing.T) {
	var calls atomic.Int64
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/querybatch" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := BatchResponse{Results: make([]QueryResult, len(req.Queries))}
		resp.Results[0].Vulns = []Vulnerability{{ID: "GHSA-test"}}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	queries := []Query{
		{Package: PackageQuery{Name: "left-pad", Ecosystem: "npm"}, Version: "1.0.0"},
		{Package: PackageQuery{Name: "lodash", Ecosystem: "npm"}, Version: "4.17.21"},
	}

	results, err := c.QueryBatch(context.Background(), queries, false)
	if err != nil {
		t.Fatalf("QueryBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if len(results[0].Vulns) != 1 || results[0].Vulns[0].ID != "GHSA-test" {
		t.Errorf("unexpected first result: %+v", results[0])
	}

	// Second identical call should be served from cache.
	if _, err := c.QueryBatch(context.Background(), queries, false); err != nil {
		t.Fatalf("cached QueryBatch: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server called %d times, want 1 (cache hit expected)", got)
	}

	// refresh=true bypasses the cache.
	if _, err := c.QueryBatch(context.Background(), queries, true); err != nil {
		t.Fatalf("refresh QueryBatch: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 after refresh", got)
	}
}

func TestQueryBatch_ServerError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"secret":"do-not-leak"}`, http.StatusBadRequest)
	}))

	_, err := c.QueryBatch(context.Background(), []Query{{Package: PackageQuery{Name: "x", Ecosystem: "npm"}, Version: "1.0.0"}}, false)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestGetVulnerability_RoundTripAndCache(t *testing.T) {
	var calls atomic.Int64
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/vulns/GHSA-abcd" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Vulnerability{ID: "GHSA-abcd", Summary: "test vuln"})
	}))

	vuln, err := c.GetVulnerability(context.Background(), "GHSA-abcd", false)
	if err != nil {
		t.Fatalf("GetVulnerability: %v", err)
	}
	if vuln.ID != "GHSA-abcd" || vuln.Summary != "test vuln" {
		t.Errorf("unexpected vuln: %+v", vuln)
	}

	if _, err := c.GetVulnerability(context.Background(), "GHSA-abcd", false); err != nil {
		t.Fatalf("cached GetVulnerability: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server called %d times, want 1 (cache hit expected)", got)
	}

	if _, err := c.GetVulnerability(context.Background(), "", false); err == nil {
		t.Error("expected error for empty id")
	}
}

func TestQueryBatch_RepairRecachesCorrectedResult(t *testing.T) {
	// First response has the wrong number of results (simulating a stale or
	// malformed cache entry being created); subsequent responses are correct.
	// The repair path must write the corrected result back to the cache so
	// later calls don't repeat the repair.
	var calls atomic.Int64
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var req BatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := BatchResponse{Results: make([]QueryResult, len(req.Queries))}
		if n == 1 {
			resp.Results = resp.Results[:1] // wrong length on first fetch
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	queries := []Query{
		{Package: PackageQuery{Name: "left-pad", Ecosystem: "npm"}, Version: "1.0.0"},
		{Package: PackageQuery{Name: "lodash", Ecosystem: "npm"}, Version: "4.17.21"},
	}

	results, err := c.QueryBatch(context.Background(), queries, false)
	if err != nil {
		t.Fatalf("QueryBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results after repair, want 2", len(results))
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server called %d times, want 2 (initial + repair)", got)
	}

	// The corrected result must now be served from cache without refetching.
	results, err = c.QueryBatch(context.Background(), queries, false)
	if err != nil {
		t.Fatalf("cached QueryBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d cached results, want 2", len(results))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 (repair result should be cached)", got)
	}
}

func TestBatchCacheKey_OrderIndependent(t *testing.T) {
	a := []Query{
		{Package: PackageQuery{Name: "a", Ecosystem: "npm"}, Version: "1"},
		{Package: PackageQuery{Name: "b", Ecosystem: "npm"}, Version: "2"},
	}
	b := []Query{a[1], a[0]}

	if batchCacheKey(a) != batchCacheKey(b) {
		t.Error("cache key should be independent of query order")
	}

	c := []Query{a[0], {Package: PackageQuery{Name: "b", Ecosystem: "npm"}, Version: "3"}}
	if batchCacheKey(a) == batchCacheKey(c) {
		t.Error("different queries should produce different cache keys")
	}
}
