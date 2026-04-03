package npm

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

func TestClient_FetchPackage(t *testing.T) {
	response := registryResponse{
		Name: "express",
		DistTags: distTags{
			Latest: "4.18.0",
		},
		Versions: map[string]versionDetails{
			"4.18.0": {
				Description: "Fast, unopinionated web framework",
				License:     "MIT",
				Author:      "TJ Holowaychuk",
				Repository: map[string]interface{}{
					"type": "git",
					"url":  "git+https://github.com/expressjs/express.git",
				},
				HomePage: "https://expressjs.com",
				Dependencies: map[string]string{
					"body-parser": "1.20.0",
					"cookie":      "0.5.0",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/express" {
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchPackage(context.Background(), "express", true)
	if err != nil {
		t.Fatalf("FetchPackage failed: %v", err)
	}

	if info.Name != "express" {
		t.Errorf("expected name express, got %s", info.Name)
	}
	if info.Version != "4.18.0" {
		t.Errorf("expected version 4.18.0, got %s", info.Version)
	}
	if len(info.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(info.Dependencies))
	}
	if info.Repository != "https://github.com/expressjs/express" {
		t.Errorf("expected normalized repo URL, got %s", info.Repository)
	}
}

func TestClient_FetchPackage_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	_, err := c.FetchPackage(context.Background(), "nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent package")
	}
}

func TestClient_ListVersions_ToleratesMalformedEngines(t *testing.T) {
	response := map[string]any{
		"name": "mongoose",
		"dist-tags": map[string]any{
			"latest": "8.0.0",
		},
		"versions": map[string]any{
			// Real-world malformed shape: engines is an array instead of an object.
			"7.0.0": map[string]any{
				"engines": []any{"node >=14"},
			},
			"8.0.0": map[string]any{
				"engines": map[string]any{"node": ">=16"},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mongoose" {
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	versions, err := c.ListVersions(context.Background(), "mongoose", true)
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d (%v)", len(versions), versions)
	}
}

func TestClient_FetchPackageVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/express/4.18.2" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"description": "Express specific version",
				"engines":     map[string]any{"node": ">=18"},
				"dependencies": map[string]string{
					"qs": "^6.13.0",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchPackageVersion(context.Background(), "express", "4.18.2", true)
	if err != nil {
		t.Fatalf("FetchPackageVersion failed: %v", err)
	}
	if info.Version != "4.18.2" {
		t.Fatalf("expected version 4.18.2, got %s", info.Version)
	}
	if info.RequiredNode != ">=18" {
		t.Fatalf("expected required node >=18, got %s", info.RequiredNode)
	}
}

func TestClient_ListVersionsWithConstraints(t *testing.T) {
	response := map[string]any{
		"name": "express",
		"versions": map[string]any{
			"4.18.2": map[string]any{"engines": map[string]any{"node": ">=18"}},
			"4.17.0": map[string]any{"engines": map[string]any{"node": ">=14"}},
			"3.0.0":  map[string]any{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/express" {
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	got, err := c.ListVersionsWithConstraints(context.Background(), "express", true)
	if err != nil {
		t.Fatalf("ListVersionsWithConstraints failed: %v", err)
	}
	if got["4.18.2"] != ">=18" {
		t.Fatalf("expected >=18 for 4.18.2, got %q", got["4.18.2"])
	}
	if got["3.0.0"] != "" {
		t.Fatalf("expected empty constraint for 3.0.0, got %q", got["3.0.0"])
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"git+https", "git+https://github.com/user/repo.git", "https://github.com/user/repo"},
		{"git protocol", "git://github.com/user/repo.git", "https://github.com/user/repo"},
		{"ssh format", "git@github.com:user/repo.git", "https://github.com/user/repo"},
		{"plain https", "https://github.com/user/repo", "https://github.com/user/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := integrations.NormalizeRepoURL(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestExtractField(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		field    string
		expected string
	}{
		{"string", "MIT", "type", "MIT"},
		{"object", map[string]interface{}{"type": "MIT"}, "type", "MIT"},
		{"nil", nil, "type", ""},
		{"empty object", map[string]interface{}{}, "type", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractField(tt.input, tt.field)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "npm:", time.Hour, nil),
		baseURL: serverURL,
	}
}
