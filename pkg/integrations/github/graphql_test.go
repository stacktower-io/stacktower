package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"

	"github.com/stacktower-io/stacktower/pkg/integrations"
)

// testCachedClient builds a Client backed by a real file cache so that
// batch caching behavior can be observed.
func testCachedClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	backend, err := cache.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	return &Client{
		Client:  integrations.NewClient(backend, "github:", time.Hour, nil),
		baseURL: serverURL,
	}
}

func TestFetchBatch_NullReposOmitted(t *testing.T) {
	// Repos sort alphabetically inside the batch: r0 = aaa/exists, r1 = bbb/missing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"r0":{"stargazerCount":42,"description":"found"},"r1":null}}`))
	}))
	defer server.Close()

	c := testClient(t, server.URL, "")

	result, err := c.FetchBatch(context.Background(), []RepoID{
		{Owner: "aaa", Name: "exists"},
		{Owner: "bbb", Name: "missing"},
	}, true)
	if err != nil {
		t.Fatalf("FetchBatch: %v", err)
	}

	m, ok := result["aaa/exists"]
	if !ok {
		t.Fatal("expected aaa/exists in result")
	}
	if m.Stars != 42 {
		t.Errorf("stars = %d, want 42", m.Stars)
	}
	if _, ok := result["bbb/missing"]; ok {
		t.Error("null repo should be omitted from result, not returned with zero values")
	}
}

func TestFetchBatch_SkipsInvalidRepos(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotQuery = req.Query
		_, _ = w.Write([]byte(`{"data":{"r0":{"stargazerCount":1}}}`))
	}))
	defer server.Close()

	c := testClient(t, server.URL, "")

	result, err := c.FetchBatch(context.Background(), []RepoID{
		{Owner: "good", Name: "repo"},
		{Owner: "bad owner", Name: "repo"},       // space in owner
		{Owner: "owner\"){evil", Name: "inject"}, // GraphQL injection attempt
		{Owner: "", Name: "repo"},                // empty owner
	}, true)
	if err != nil {
		t.Fatalf("FetchBatch: %v", err)
	}

	if !strings.Contains(gotQuery, `owner: "good"`) {
		t.Errorf("query should contain the valid repo, got: %s", gotQuery)
	}
	for _, bad := range []string{"bad owner", "evil", "inject"} {
		if strings.Contains(gotQuery, bad) {
			t.Errorf("query should not contain invalid repo %q, got: %s", bad, gotQuery)
		}
	}
	if _, ok := result["good/repo"]; !ok {
		t.Error("expected good/repo in result")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d: %v", len(result), result)
	}
}

func TestFetchBatch_CacheKeyOrderIndependent(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"r0":{"stargazerCount":1},"r1":{"stargazerCount":2}}}`))
	}))
	defer server.Close()

	c := testCachedClient(t, server.URL)

	repos := []RepoID{
		{Owner: "aaa", Name: "first"},
		{Owner: "bbb", Name: "second"},
	}
	reversed := []RepoID{repos[1], repos[0]}

	first, err := c.FetchBatch(context.Background(), repos, false)
	if err != nil {
		t.Fatalf("FetchBatch: %v", err)
	}
	second, err := c.FetchBatch(context.Background(), reversed, false)
	if err != nil {
		t.Fatalf("FetchBatch (reversed): %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("server called %d times, want 1 (same set in different order should hit cache)", got)
	}
	if first["aaa/first"] == nil || second["aaa/first"] == nil {
		t.Fatalf("missing aaa/first: first=%v second=%v", first, second)
	}
	if first["aaa/first"].Stars != second["aaa/first"].Stars {
		t.Errorf("cached result mismatch: %d vs %d", first["aaa/first"].Stars, second["aaa/first"].Stars)
	}
}
