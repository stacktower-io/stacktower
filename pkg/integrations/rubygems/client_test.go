package rubygems

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"

	"github.com/matzehuels/stacktower/pkg/integrations"
)

func TestClient_FetchGem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gems/rails.json" {
			resp := gemResponse{
				Name:          "rails",
				Version:       "7.1.0",
				Info:          "Ruby on Rails is a full-stack web framework",
				Licenses:      []string{"MIT"},
				SourceCodeURI: "https://github.com/rails/rails",
				HomepageURI:   "https://rubyonrails.org",
				Authors:       "David Heinemeier Hansson",
				Downloads:     500000000,
			}
			resp.Dependencies.Runtime = []dependency{
				{Name: "activesupport"},
				{Name: "actionpack"},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchGem(context.Background(), "rails", true)
	if err != nil {
		t.Fatalf("FetchGem failed: %v", err)
	}

	if info.Name != "rails" {
		t.Errorf("expected name rails, got %s", info.Name)
	}
	if info.Version != "7.1.0" {
		t.Errorf("expected version 7.1.0, got %s", info.Version)
	}
	if len(info.Dependencies) != 2 {
		t.Errorf("expected 2 runtime dependencies, got %d", len(info.Dependencies))
	}
	if info.License != "MIT" {
		t.Errorf("expected license MIT, got %s", info.License)
	}
}

func TestClient_FetchGem_NotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	c := testClient(t, server.URL)

	_, err := c.FetchGem(context.Background(), "missing-gem", true)
	if err == nil {
		t.Fatal("expected error for missing gem")
	}
	if !errors.Is(err, integrations.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_FetchGemVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/versions/rails.json" {
			_ = json.NewEncoder(w).Encode([]gemVersionResponse{
				{
					Number:              "7.1.0",
					RequiredRubyVersion: ">= 3.0.0",
				},
				{
					Number:              "7.0.0",
					RequiredRubyVersion: ">= 2.7.0",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchGemVersion(context.Background(), "rails", "7.0.0", true)
	if err != nil {
		t.Fatalf("FetchGemVersion failed: %v", err)
	}
	if info.Version != "7.0.0" {
		t.Fatalf("expected version 7.0.0, got %s", info.Version)
	}
	if info.RequiredRubyVersion != ">= 2.7.0" {
		t.Fatalf("expected ruby constraint >= 2.7.0, got %s", info.RequiredRubyVersion)
	}
}

func TestClient_ListVersionsWithConstraints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/versions/rails.json" {
			_ = json.NewEncoder(w).Encode([]gemVersionResponse{
				{Number: "7.1.0", RequiredRubyVersion: ">= 3.0.0"},
				{Number: "7.0.0", RequiredRubyVersion: ">= 2.7.0"},
				{Number: "6.0.0"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	got, err := c.ListVersionsWithConstraints(context.Background(), "rails", true)
	if err != nil {
		t.Fatalf("ListVersionsWithConstraints failed: %v", err)
	}
	if got["7.1.0"] != ">= 3.0.0" {
		t.Fatalf("expected >= 3.0.0, got %q", got["7.1.0"])
	}
	if got["6.0.0"] != "" {
		t.Fatalf("expected empty constraint, got %q", got["6.0.0"])
	}
}

func TestRuntimeDeps(t *testing.T) {
	deps := []dependency{
		{Name: "activesupport"},
		{Name: "actionpack"},
		{Name: "ActionPack"}, // duplicate after normalization
	}

	result := runtimeDeps(deps)
	if len(result) != 2 {
		t.Errorf("expected 2 unique deps, got %d", len(result))
	}
}

func TestJoinLicenses(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"MIT"}, "MIT"},
		{[]string{"MIT", "Apache-2.0"}, "MIT, Apache-2.0"},
	}

	for _, tt := range tests {
		result := strings.Join(tt.input, ", ")
		if result != tt.expected {
			t.Errorf("join(%v): expected %s, got %s", tt.input, tt.expected, result)
		}
	}
}

func TestRubyVersionsEquivalent(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"3.17.0.0", "3.17.0", true},
		{"v3.17.0.0", "3.17.0", true},
		{"3.17.0.5", "3.17.0", false},
		{"1.2.3", "1.2.3", true},
	}
	for _, tt := range tests {
		if got := rubyVersionsEquivalent(tt.a, tt.b); got != tt.want {
			t.Fatalf("rubyVersionsEquivalent(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "rubygems:", time.Hour, nil),
		baseURL: serverURL,
	}
}
