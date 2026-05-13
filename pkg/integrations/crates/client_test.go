package crates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"

	"github.com/stacktower-io/stacktower/pkg/integrations"
)

func TestNewClient(t *testing.T) {
	c := NewClient(cache.NewNullCache(), time.Hour)
	if c.Client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestSparseIndexURL(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"a", "https://index.crates.io/1/a"},
		{"ab", "https://index.crates.io/2/ab"},
		{"abc", "https://index.crates.io/3/a/abc"},
		{"serde", "https://index.crates.io/se/rd/serde"},
		{"tokio", "https://index.crates.io/to/ki/tokio"},
		{"thiserror", "https://index.crates.io/th/is/thiserror"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sparseIndexURL(tt.name)
			if got != tt.want {
				t.Errorf("sparseIndexURL(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func makeIndexNDJSON(entries ...indexEntry) string {
	var lines []string
	for _, e := range entries {
		b, _ := json.Marshal(e)
		lines = append(lines, string(b))
	}
	return strings.Join(lines, "\n")
}

func TestClient_FetchCrate(t *testing.T) {
	crateResp := crateResponse{}
	crateResp.Crate.Name = "serde"
	crateResp.Crate.MaxVersion = "1.0.0"
	crateResp.Crate.Description = "A serialization framework"
	crateResp.Crate.License = "MIT"
	crateResp.Crate.Repository = "https://github.com/serde-rs/serde"
	crateResp.Crate.Downloads = 1000000

	pkg := func(p *string) *string { return p }
	_ = pkg

	indexData := makeIndexNDJSON(
		indexEntry{
			Name:    "serde",
			Version: "1.0.0",
			Deps: []indexDep{
				{Name: "serde_derive", Req: "^1.0", Kind: "normal", Optional: false},
				{Name: "test_dep", Req: "^0.1", Kind: "dev", Optional: false},
				{Name: "optional_dep", Req: "^2.0", Kind: "normal", Optional: true},
			},
		},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crates/serde":
			json.NewEncoder(w).Encode(crateResp)
		case isIndexPath(r.URL.Path, "serde"):
			fmt.Fprint(w, indexData)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)

	info, err := c.FetchCrate(context.Background(), "serde", true)
	if err != nil {
		t.Fatalf("FetchCrate failed: %v", err)
	}

	if info.Name != "serde" {
		t.Errorf("expected name serde, got %s", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", info.Version)
	}
	if len(info.Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(info.Dependencies))
	}
	if len(info.Dependencies) > 0 {
		if info.Dependencies[0].Name != "serde_derive" {
			t.Errorf("expected serde_derive, got %s", info.Dependencies[0].Name)
		}
		if info.Dependencies[0].Constraint != "^1.0" {
			t.Errorf("expected constraint ^1.0, got %s", info.Dependencies[0].Constraint)
		}
	}
}

func TestClient_FetchCrate_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)

	_, err := c.FetchCrate(context.Background(), "nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent crate")
	}
}

func TestClient_FetchCrateVersion(t *testing.T) {
	crateResp := crateResponse{}
	crateResp.Crate.Name = "serde"
	crateResp.Crate.MaxVersion = "1.0.0"

	indexData := makeIndexNDJSON(
		indexEntry{Name: "serde", Version: "1.0.0", RustVersion: "1.65.0"},
		indexEntry{Name: "serde", Version: "1.0.1", RustVersion: "1.70.0"},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crates/serde":
			_ = json.NewEncoder(w).Encode(crateResp)
		case isIndexPath(r.URL.Path, "serde"):
			fmt.Fprint(w, indexData)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)
	info, err := c.FetchCrateVersion(context.Background(), "serde", "1.0.1", true)
	if err != nil {
		t.Fatalf("FetchCrateVersion failed: %v", err)
	}
	if info.Version != "1.0.1" {
		t.Fatalf("expected version 1.0.1, got %s", info.Version)
	}
	if info.MSRV != "1.70.0" {
		t.Fatalf("expected MSRV 1.70.0, got %s", info.MSRV)
	}
}

func TestClient_FetchCrateVersionFromIndex(t *testing.T) {
	indexData := makeIndexNDJSON(
		indexEntry{
			Name:        "serde",
			Version:     "1.0.228",
			RustVersion: "1.31",
			Deps: []indexDep{
				{Name: "serde_derive", Req: "^1.0.228", Kind: "normal", Optional: false},
			},
		},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isIndexPath(r.URL.Path, "serde") {
			fmt.Fprint(w, indexData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)
	info, err := c.FetchCrateVersionFromIndex(context.Background(), "serde", "1.0.228", true)
	if err != nil {
		t.Fatalf("FetchCrateVersionFromIndex failed: %v", err)
	}
	if info.Version != "1.0.228" {
		t.Fatalf("expected version 1.0.228, got %s", info.Version)
	}
	if info.MSRV != "1.31" {
		t.Fatalf("expected MSRV 1.31, got %s", info.MSRV)
	}
	if len(info.Dependencies) != 1 || info.Dependencies[0].Name != "serde_derive" {
		t.Fatalf("expected serde_derive dep, got %v", info.Dependencies)
	}
	if info.Description != "" {
		t.Fatalf("expected empty description from index-only fetch, got %q", info.Description)
	}
}

func TestClient_ListVersionsWithConstraints(t *testing.T) {
	indexData := makeIndexNDJSON(
		indexEntry{Name: "serde", Version: "1.0.1", RustVersion: "1.70.0"},
		indexEntry{Name: "serde", Version: "1.0.0", RustVersion: "1.65.0"},
		indexEntry{Name: "serde", Version: "0.9.0", RustVersion: "1.56.0", Yanked: true},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isIndexPath(r.URL.Path, "serde") {
			fmt.Fprint(w, indexData)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)
	got, err := c.ListVersionsWithConstraints(context.Background(), "serde", true)
	if err != nil {
		t.Fatalf("ListVersionsWithConstraints failed: %v", err)
	}
	if got["1.0.1"] != "1.70.0" {
		t.Fatalf("expected 1.70.0 for 1.0.1, got %q", got["1.0.1"])
	}
	if _, exists := got["0.9.0"]; exists {
		t.Fatalf("expected yanked version 0.9.0 to be excluded")
	}
}

func TestClient_IndexOnlyDuringSolving(t *testing.T) {
	restHits := 0
	indexHits := 0

	indexData := makeIndexNDJSON(
		indexEntry{
			Name:        "serde",
			Version:     "1.0.1",
			RustVersion: "1.70.0",
			Deps: []indexDep{
				{Name: "serde_derive", Req: "^1.0", Kind: "normal"},
			},
		},
		indexEntry{Name: "serde", Version: "1.0.0", RustVersion: "1.65.0"},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isIndexPath(r.URL.Path, "serde"):
			indexHits++
			fmt.Fprint(w, indexData)
		case r.URL.Path == "/crates/serde":
			restHits++
			resp := crateResponse{}
			resp.Crate.Name = "serde"
			resp.Crate.MaxVersion = "1.0.1"
			json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testCachedClientWithIndex(t, server.URL)

	// Simulate solving: ListVersions + FetchCrateVersionFromIndex
	if _, err := c.ListVersions(context.Background(), "serde", false); err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if _, err := c.ListVersionsWithConstraints(context.Background(), "serde", false); err != nil {
		t.Fatalf("ListVersionsWithConstraints failed: %v", err)
	}
	info, err := c.FetchCrateVersionFromIndex(context.Background(), "serde", "1.0.1", false)
	if err != nil {
		t.Fatalf("FetchCrateVersionFromIndex failed: %v", err)
	}
	if info.MSRV != "1.70.0" {
		t.Fatalf("expected MSRV 1.70.0, got %q", info.MSRV)
	}

	// During solving: only the index should be hit, zero REST API calls
	if indexHits != 1 {
		t.Fatalf("sparse index hit %d times, want 1", indexHits)
	}
	if restHits != 0 {
		t.Fatalf("REST API hit %d times during solving, want 0", restHits)
	}

	// Now simulate DAG enrichment: FetchCrateVersion (needs REST API)
	full, err := c.FetchCrateVersion(context.Background(), "serde", "1.0.1", false)
	if err != nil {
		t.Fatalf("FetchCrateVersion failed: %v", err)
	}
	if full.Name != "serde" {
		t.Fatalf("expected name serde, got %s", full.Name)
	}
	if restHits != 1 {
		t.Fatalf("REST API hit %d times after enrichment, want 1", restHits)
	}
}

func TestClient_LicenseFallbackToVersion(t *testing.T) {
	crateResp := crateResponse{}
	crateResp.Crate.Name = "dsl_auto_type"
	crateResp.Crate.MaxVersion = "0.1.0"
	crateResp.Crate.License = "" // Empty at crate level

	indexData := makeIndexNDJSON(
		indexEntry{Name: "dsl_auto_type", Version: "0.1.0", License: "MIT OR Apache-2.0"},
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/crates/dsl_auto_type":
			_ = json.NewEncoder(w).Encode(crateResp)
		case isIndexPath(r.URL.Path, "dsl_auto_type"):
			fmt.Fprint(w, indexData)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClientWithIndex(t, server.URL)
	info, err := c.FetchCrate(context.Background(), "dsl_auto_type", true)
	if err != nil {
		t.Fatalf("FetchCrate failed: %v", err)
	}
	if info.License != "MIT OR Apache-2.0" {
		t.Errorf("expected license 'MIT OR Apache-2.0' from index fallback, got %q", info.License)
	}
}

// isIndexPath checks if the URL path matches the sparse index pattern for a crate.
func isIndexPath(path, crate string) bool {
	name := strings.ToLower(crate)
	n := len(name)
	var expected string
	switch n {
	case 1:
		expected = "/1/" + name
	case 2:
		expected = "/2/" + name
	case 3:
		expected = "/3/" + name[:1] + "/" + name
	default:
		expected = "/" + name[:2] + "/" + name[2:4] + "/" + name
	}
	return path == expected
}

func testClientWithIndex(t *testing.T, serverURL string) *Client {
	t.Helper()
	headers := map[string]string{
		"User-Agent": "stacktower/1.0 (https://github.com/stacktower-io/stacktower)",
	}
	return &Client{
		Client:   integrations.NewClient(cache.NewNullCache(), "crates:", time.Hour, headers),
		baseURL:  serverURL,
		indexURL: serverURL,
	}
}

func testCachedClientWithIndex(t *testing.T, serverURL string) *Client {
	t.Helper()
	backend, err := cache.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	headers := map[string]string{
		"User-Agent": "stacktower/1.0 (https://github.com/stacktower-io/stacktower)",
	}
	return &Client{
		Client:   integrations.NewClient(backend, "crates:", time.Hour, headers),
		baseURL:  serverURL,
		indexURL: serverURL,
	}
}
