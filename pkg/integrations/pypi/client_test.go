package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"

	"github.com/stacktower-io/stacktower/pkg/integrations"
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

func TestEvaluatePythonVersionMarker(t *testing.T) {
	tests := []struct {
		name          string
		marker        string
		pythonVersion string
		want          bool
	}{
		{"no python_version", "os_name == 'posix'", "3.11", true},
		{"simple >=", "python_version >= '3.8'", "3.11", true},
		{"simple < excluded", "python_version < '3.11'", "3.11", false},
		{"AND both satisfied", "python_version >= '3.8' and python_version < '4.0'", "3.11", true},
		{"AND one fails", "python_version >= '3.8' and python_version < '3.10'", "3.11", false},
		{"OR first matches", "python_version < '3.8' or python_version >= '3.11'", "3.11", true},
		{"OR second matches", "python_version >= '3.12' or python_version >= '3.11'", "3.11", true},
		{"OR neither matches", "python_version < '3.8' or python_version == '3.10'", "3.11", false},
		{"empty marker", "", "3.11", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluatePythonVersionMarker(tt.marker, tt.pythonVersion)
			if got != tt.want {
				t.Errorf("evaluatePythonVersionMarker(%q, %q) = %v, want %v",
					tt.marker, tt.pythonVersion, got, tt.want)
			}
		})
	}
}

func TestEvaluateMarker_EnvMarkers(t *testing.T) {
	linuxEnv := map[string]string{
		"sys_platform":     "linux",
		"os_name":          "posix",
		"platform_machine": "x86_64",
	}
	windowsEnv := map[string]string{
		"sys_platform":     "win32",
		"os_name":          "nt",
		"platform_machine": "AMD64",
	}

	tests := []struct {
		name   string
		marker string
		env    map[string]string
		want   bool
	}{
		{"sys_platform match", `sys_platform == "linux"`, linuxEnv, true},
		{"sys_platform mismatch", `sys_platform == "win32"`, linuxEnv, false},
		{"sys_platform negated", `sys_platform != "win32"`, linuxEnv, true},
		{"os_name nt on windows", `os_name == "nt"`, windowsEnv, true},
		{"os_name nt on linux", `os_name == "nt"`, linuxEnv, false},
		{"platform_machine match", `platform_machine == "x86_64"`, linuxEnv, true},
		{"platform_machine mismatch", `platform_machine == "aarch64"`, linuxEnv, false},
		{"combined with python_version pass", `sys_platform == "linux" and python_version >= "3.8"`, linuxEnv, true},
		{"combined with python_version fail platform", `sys_platform == "win32" and python_version >= "3.8"`, linuxEnv, false},
		{"or across platforms", `sys_platform == "win32" or sys_platform == "linux"`, linuxEnv, true},
		{"unknown marker ignored", `implementation_name == "cpython"`, linuxEnv, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateMarker(tt.marker, "3.11", tt.env)
			if got != tt.want {
				t.Errorf("evaluateMarker(%q) = %v, want %v", tt.marker, got, tt.want)
			}
		})
	}
}

func TestDefaultEnvMarkers(t *testing.T) {
	env := defaultEnvMarkers()
	for _, key := range []string{"sys_platform", "os_name", "platform_machine"} {
		if env[key] == "" {
			t.Errorf("defaultEnvMarkers()[%q] is empty", key)
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

func TestClient_FetchPackageVersion_EscapesVersion(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		resp := apiResponse{Info: apiInfo{Name: "torch", Version: "2.1.0+cu118"}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	// PEP 440 local version: '+' must survive path interpolation intact.
	info, err := c.FetchPackageVersion(context.Background(), "torch", "2.1.0+cu118", true)
	if err != nil {
		t.Fatalf("FetchPackageVersion failed: %v", err)
	}
	if info.Version != "2.1.0+cu118" {
		t.Errorf("version = %q, want 2.1.0+cu118", info.Version)
	}
	if gotPath != "/torch/2.1.0+cu118/json" {
		t.Errorf("request path = %q, want /torch/2.1.0+cu118/json", gotPath)
	}

	// Path metacharacters must be escaped so they can't alter the URL structure.
	if _, err := c.FetchPackageVersion(context.Background(), "torch", "1.0/evil", true); err != nil {
		t.Fatalf("FetchPackageVersion failed: %v", err)
	}
	if gotPath != "/torch/1.0%2Fevil/json" {
		t.Errorf("request path = %q, want /torch/1.0%%2Fevil/json", gotPath)
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
		// Full PEP 503: runs of [-_.] collapse to a single hyphen.
		{"zope.interface", "zope-interface"},
		{"my--pkg__name", "my-pkg-name"},
		{"a-_.b", "a-b"},
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
