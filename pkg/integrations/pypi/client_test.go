package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"

	"github.com/matzehuels/stacktower/pkg/integrations"
)

func TestClient_FetchPackage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/flask/json" {
			resp := apiResponse{
				Info: apiInfo{
					Name:         "Flask",
					Version:      "2.0.0",
					Summary:      "A micro web framework",
					License:      "BSD-3-Clause",
					RequiresDist: []string{"click>=7.0", "werkzeug>=2.0"},
					ProjectURLs: map[string]any{
						"Source": "https://github.com/pallets/flask",
					},
					Author: "Armin Ronacher",
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchPackage(context.Background(), "flask", true)
	if err != nil {
		t.Fatalf("FetchPackage failed: %v", err)
	}

	if info.Name != "Flask" {
		t.Errorf("expected name Flask, got %s", info.Name)
	}
	if info.Version == "" {
		t.Error("expected non-empty version")
	}
	if len(info.Dependencies) == 0 {
		t.Error("expected at least one dependency")
	}
}

func TestClient_FetchPackage_NotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	c := testClient(t, server.URL)

	_, err := c.FetchPackage(context.Background(), "missing-pkg", true)
	if err == nil {
		t.Fatal("expected error for missing package")
	}
	if !errors.Is(err, integrations.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestExtractDeps_FiltersMarkers(t *testing.T) {
	tests := []struct {
		input    []string
		expected int
	}{
		{[]string{"requests", "numpy; extra == 'dev'"}, 1},
		{[]string{"django>=3.0", "pytest; extra == 'test'"}, 1},
		{[]string{"flask"}, 1},
		// python_version markers: should filter deps for older Python versions
		{[]string{"sniffio>=1.1; python_version < '3.11'"}, 0},        // Excluded: requires Python < 3.11
		{[]string{"contextvars; python_version < '3.7'"}, 0},          // Excluded: requires Python < 3.7
		{[]string{"typing-extensions; python_version >= '3.8'"}, 1},   // Included: satisfied on 3.11
		{[]string{"exceptiongroup>=1.0; python_version < '3.11'"}, 0}, // Excluded: requires Python < 3.11
	}

	c := testExtractClient()
	for _, tt := range tests {
		got := c.extractDeps(tt.input)
		if len(got) != tt.expected {
			t.Errorf("extractDeps(%v): expected %d deps, got %d", tt.input, tt.expected, len(got))
		}
	}
}

func TestExtractDeps_ExtractsConstraints(t *testing.T) {
	tests := []struct {
		name           string
		input          []string
		wantName       string
		wantConstraint string
	}{
		{
			name:           "simple constraint",
			input:          []string{"requests>=2.0"},
			wantName:       "requests",
			wantConstraint: ">=2.0",
		},
		{
			name:           "range constraint",
			input:          []string{"httpx>=0.23.0,<1"},
			wantName:       "httpx",
			wantConstraint: ">=0.23.0,<1",
		},
		{
			name:           "exact version",
			input:          []string{"numpy==1.24.0"},
			wantName:       "numpy",
			wantConstraint: "==1.24.0",
		},
		{
			name:           "compatible release",
			input:          []string{"django~=4.2"},
			wantName:       "django",
			wantConstraint: "~=4.2",
		},
		{
			name:           "no constraint",
			input:          []string{"flask"},
			wantName:       "flask",
			wantConstraint: "",
		},
		{
			name:           "with extras",
			input:          []string{"requests[security]>=2.0"},
			wantName:       "requests",
			wantConstraint: ">=2.0",
		},
		{
			name:           "constraint with spaces",
			input:          []string{"click >= 7.0"},
			wantName:       "click",
			wantConstraint: ">= 7.0",
		},
		{
			name:           "complex constraint with marker",
			input:          []string{"typing-extensions>=4.7,<5; python_version >= '3.8'"},
			wantName:       "typing-extensions",
			wantConstraint: ">=4.7,<5",
		},
	}

	c := testExtractClient()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := c.extractDeps(tt.input)
			if len(deps) != 1 {
				t.Fatalf("expected 1 dep, got %d", len(deps))
			}
			if deps[0].Name != tt.wantName {
				t.Errorf("name = %q, want %q", deps[0].Name, tt.wantName)
			}
			if deps[0].Constraint != tt.wantConstraint {
				t.Errorf("constraint = %q, want %q", deps[0].Constraint, tt.wantConstraint)
			}
		})
	}
}

func TestNormalizePkgName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Django", "django"},
		{"Flask_App", "flask-app"},
		{"some_package-name", "some-package-name"},
		{"UPPERCASE", "uppercase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := integrations.NormalizePkgName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		Client:        integrations.NewClient(cache.NewNullCache(), "pypi:", time.Hour, nil),
		baseURL:       serverURL,
		pythonVersion: DefaultPythonVersion,
	}
}

// testExtractClient returns a minimal client for testing extractDeps.
func testExtractClient() *Client {
	return &Client{pythonVersion: DefaultPythonVersion}
}
