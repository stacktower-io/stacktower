package crates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"

	"github.com/matzehuels/stacktower/pkg/integrations"
)

func TestNewClient(t *testing.T) {
	c := NewClient(cache.NewNullCache(), time.Hour)
	if c.Client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestClient_FetchCrate(t *testing.T) {
	crateResp := crateResponse{}
	crateResp.Crate.Name = "serde"
	crateResp.Crate.MaxVersion = "1.0.0"
	crateResp.Crate.Description = "A serialization framework"
	crateResp.Crate.License = "MIT"
	crateResp.Crate.Repository = "https://github.com/serde-rs/serde"
	crateResp.Crate.Downloads = 1000000

	depsResp := depsResponse{
		Dependencies: []struct {
			CrateID  string `json:"crate_id"`
			Kind     string `json:"kind"`
			Optional bool   `json:"optional"`
			Req      string `json:"req"`
		}{
			{CrateID: "serde_derive", Kind: "normal", Optional: false, Req: "^1.0"},
			{CrateID: "test_dep", Kind: "dev", Optional: false, Req: "^0.1"},
			{CrateID: "optional_dep", Kind: "normal", Optional: true, Req: "^2.0"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crates/serde":
			json.NewEncoder(w).Encode(crateResp)
		case "/crates/serde/1.0.0/dependencies":
			json.NewEncoder(w).Encode(depsResp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)

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

	c := testClient(t, server.URL)

	_, err := c.FetchCrate(context.Background(), "nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent crate")
	}
}

func TestClient_FetchCrateVersion(t *testing.T) {
	crateResp := crateResponse{}
	crateResp.Crate.Name = "serde"
	crateResp.Crate.MaxVersion = "1.0.0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crates/serde":
			_ = json.NewEncoder(w).Encode(crateResp)
		case "/crates/serde/1.0.1/dependencies":
			_ = json.NewEncoder(w).Encode(depsResponse{})
		case "/crates/serde/versions":
			_ = json.NewEncoder(w).Encode(versionsResponse{
				Versions: []versionInfo{
					{Num: "1.0.1", RustVersion: "1.70.0"},
					{Num: "1.0.0", RustVersion: "1.65.0"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)
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

func TestClient_ListVersionsWithConstraints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crates/serde/versions" {
			_ = json.NewEncoder(w).Encode(versionsResponse{
				Versions: []versionInfo{
					{Num: "1.0.1", RustVersion: "1.70.0"},
					{Num: "1.0.0", RustVersion: "1.65.0"},
					{Num: "0.9.0", RustVersion: "1.56.0", Yanked: true},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(t, server.URL)
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

func TestClient_LicenseFallbackToVersion(t *testing.T) {
	// Some crates have null license at crate level but valid license at version level
	// (e.g., dsl_auto_type). We should fall back to version-level license.
	crateResp := crateResponse{}
	crateResp.Crate.Name = "dsl_auto_type"
	crateResp.Crate.MaxVersion = "0.1.0"
	crateResp.Crate.License = "" // Empty at crate level

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crates/dsl_auto_type":
			_ = json.NewEncoder(w).Encode(crateResp)
		case "/crates/dsl_auto_type/0.1.0/dependencies":
			_ = json.NewEncoder(w).Encode(depsResponse{})
		case "/crates/dsl_auto_type/versions":
			_ = json.NewEncoder(w).Encode(versionsResponse{
				Versions: []versionInfo{
					{Num: "0.1.0", License: "MIT OR Apache-2.0"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)
	info, err := c.FetchCrate(context.Background(), "dsl_auto_type", true)
	if err != nil {
		t.Fatalf("FetchCrate failed: %v", err)
	}
	if info.License != "MIT OR Apache-2.0" {
		t.Errorf("expected license 'MIT OR Apache-2.0' from version fallback, got %q", info.License)
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	headers := map[string]string{
		"User-Agent": "stacktower/1.0 (https://github.com/matzehuels/stacktower)",
	}
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "crates:", time.Hour, headers),
		baseURL: serverURL,
	}
}
