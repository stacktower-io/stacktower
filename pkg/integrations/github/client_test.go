package github

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

func TestClient_Fetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/owner/repo":
			json.NewEncoder(w).Encode(repoResponse{
				Stars: 100,
				Size:  500,
				License: struct {
					SPDXID string `json:"spdx_id"`
				}{SPDXID: "MIT"},
			})
		case "/repos/owner/repo/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/owner/repo/contributors":
			json.NewEncoder(w).Encode([]contributorResponse{
				{Login: "user1", Contributions: 10, Type: "User"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL, "")

	metrics, err := c.Fetch(context.Background(), "owner", "repo", true)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	if metrics.Stars != 100 {
		t.Errorf("expected 100 stars, got %d", metrics.Stars)
	}
	if metrics.SizeKB != 500 {
		t.Errorf("expected 500 KB, got %d", metrics.SizeKB)
	}
}

func TestExtractURL(t *testing.T) {
	tests := []struct {
		urls      map[string]string
		home      string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			urls:      map[string]string{"Source": "https://github.com/foo/bar"},
			wantOwner: "foo",
			wantRepo:  "bar",
			wantOK:    true,
		},
		{
			urls:      nil,
			home:      "http://github.com/baz/qux",
			wantOwner: "baz",
			wantRepo:  "qux",
			wantOK:    true,
		},
		{
			urls:   map[string]string{"Homepage": "https://google.com"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		owner, repo, ok := ExtractURL(tt.urls, tt.home)
		if ok != tt.wantOK {
			t.Errorf("got ok=%v, want %v", ok, tt.wantOK)
		}
		if ok {
			if owner != tt.wantOwner {
				t.Errorf("got owner %s, want %s", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("got repo %s, want %s", repo, tt.wantRepo)
			}
		}
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient(cache.NewNullCache(), "test-token", time.Hour)
	if c.Client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestNormalizeLicense(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MIT", "MIT"},
		{"Apache-2.0", "Apache-2.0"},
		{"PSF-2.0", "PSF-2.0"},
		{"NOASSERTION", ""}, // GitHub can't determine license
		{"OTHER", ""},       // Non-standard license detected
		{"", ""},            // No license info
	}

	for _, tt := range tests {
		got := normalizeLicense(tt.input)
		if got != tt.want {
			t.Errorf("normalizeLicense(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestClient_Fetch_NOASSERTION(t *testing.T) {
	// Test that NOASSERTION license is normalized to empty string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/repos/python/typing_extensions":
			json.NewEncoder(w).Encode(repoResponse{
				Stars: 550,
				License: struct {
					SPDXID string `json:"spdx_id"`
				}{SPDXID: "NOASSERTION"},
			})
		case "/repos/python/typing_extensions/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/python/typing_extensions/contributors":
			json.NewEncoder(w).Encode([]contributorResponse{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL, "")

	metrics, err := c.Fetch(context.Background(), "python", "typing_extensions", true)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	// NOASSERTION should be normalized to empty string
	if metrics.License != "" {
		t.Errorf("expected empty license for NOASSERTION, got %q", metrics.License)
	}
}

func testClient(t *testing.T, serverURL, token string) *Client {
	t.Helper()
	headers := map[string]string{"Accept": "application/vnd.github.v3+json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "github:", time.Hour, headers),
		baseURL: serverURL,
	}
}
