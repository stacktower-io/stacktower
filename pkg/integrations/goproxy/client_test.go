package goproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"

	"github.com/matzehuels/stacktower/pkg/integrations"
)

func TestEscapePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/gin-gonic/gin", "github.com/gin-gonic/gin"},
		{"github.com/Azure/azure-sdk-for-go", "github.com/!azure/azure-sdk-for-go"},
		{"golang.org/x/sync", "golang.org/x/sync"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := escapePath(tt.input); got != tt.want {
				t.Errorf("escapePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseGoModComplete(t *testing.T) {
	content := `module github.com/example/myapp

go 1.21

require (
	github.com/gin-gonic/gin v1.9.0
	github.com/spf13/cobra v1.7.0
	golang.org/x/sync v0.3.0 // indirect
)

require github.com/stretchr/testify v1.8.0
`

	result, err := parseGoModComplete(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseGoModComplete failed: %v", err)
	}

	// Check go version
	if result.goVersion != "1.21" {
		t.Errorf("expected go version 1.21, got %s", result.goVersion)
	}

	// Should have 3 direct deps
	if len(result.directDeps) != 3 {
		t.Errorf("expected 3 direct deps, got %d: %v", len(result.directDeps), result.directDeps)
	}

	// Should have 1 indirect dep
	if len(result.indirectDeps) != 1 {
		t.Errorf("expected 1 indirect dep, got %d: %v", len(result.indirectDeps), result.indirectDeps)
	}

	wantDirect := map[string]bool{
		"github.com/gin-gonic/gin":    true,
		"github.com/spf13/cobra":      true,
		"github.com/stretchr/testify": true,
	}
	for _, dep := range result.directDeps {
		if !wantDirect[dep.Name] {
			t.Errorf("unexpected direct dep: %s", dep.Name)
		}
	}

	// Check indirect dep
	if len(result.indirectDeps) > 0 && result.indirectDeps[0].Name != "golang.org/x/sync" {
		t.Errorf("expected indirect dep golang.org/x/sync, got %s", result.indirectDeps[0].Name)
	}
}

func TestParseRequireLineComplete(t *testing.T) {
	tests := []struct {
		line           string
		wantName       string
		wantConstraint string
		wantIndirect   bool
	}{
		{"github.com/gin-gonic/gin v1.9.0", "github.com/gin-gonic/gin", "=v1.9.0", false},
		{"golang.org/x/sync v0.3.0 // indirect", "golang.org/x/sync", "=v0.3.0", true},
		{"github.com/pkg/errors v0.9.1 // some comment", "github.com/pkg/errors", "=v0.9.1", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, isIndirect := parseRequireLineComplete(tt.line)
			if got.Name != tt.wantName {
				t.Errorf("parseRequireLineComplete(%q).Name = %q, want %q", tt.line, got.Name, tt.wantName)
			}
			if got.Constraint != tt.wantConstraint {
				t.Errorf("parseRequireLineComplete(%q).Constraint = %q, want %q", tt.line, got.Constraint, tt.wantConstraint)
			}
			if isIndirect != tt.wantIndirect {
				t.Errorf("parseRequireLineComplete(%q) isIndirect = %v, want %v", tt.line, isIndirect, tt.wantIndirect)
			}
		})
	}
}

func TestClient_FetchModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/example/mylib/@latest":
			json.NewEncoder(w).Encode(latestResponse{Version: "v1.2.3"})
		case "/github.com/example/mylib/@v/v1.2.3.mod":
			w.Write([]byte(`module github.com/example/mylib

go 1.21

require github.com/pkg/errors v0.9.1
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	info, err := c.FetchModule(context.Background(), "github.com/example/mylib", true)
	if err != nil {
		t.Fatalf("FetchModule failed: %v", err)
	}

	if info.Path != "github.com/example/mylib" {
		t.Errorf("expected path github.com/example/mylib, got %s", info.Path)
	}
	if info.Version != "v1.2.3" {
		t.Errorf("expected version v1.2.3, got %s", info.Version)
	}
	if len(info.Dependencies) != 1 {
		t.Errorf("expected 1 dep, got %d", len(info.Dependencies))
	}
	if len(info.Dependencies) > 0 && info.Dependencies[0].Name != "github.com/pkg/errors" {
		t.Errorf("expected github.com/pkg/errors, got %s", info.Dependencies[0].Name)
	}
}

func TestClient_FetchModule_NotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	c := testClient(t, server.URL)

	_, err := c.FetchModule(context.Background(), "github.com/missing/module", true)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "goproxy:", time.Hour, nil),
		baseURL: serverURL,
	}
}

func TestExtractSimpleLicense(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "MIT license link",
			html: `<a href="https://pkg.go.dev/license?lic=MIT">MIT</a>`,
			want: "MIT",
		},
		{
			name: "Apache-2.0 in content",
			html: `<span>License: Apache-2.0</span>`,
			want: "Apache-2.0",
		},
		{
			name: "BSD-3-Clause tag",
			html: `<span>License</span><span>BSD-3-Clause</span>`,
			want: "BSD-3-Clause",
		},
		{
			name: "no license",
			html: `<html><body>No license info</body></html>`,
			want: "",
		},
		{
			name: "ISC license",
			html: `>ISC<`,
			want: "ISC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSimpleLicense(tt.html)
			if got != tt.want {
				t.Errorf("extractSimpleLicense() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLicensePattern(t *testing.T) {
	html := `<a data-test-id="UnitHeader-license" href="https://pkg.go.dev/license?lic=MIT">MIT</a>`
	matches := licensePattern.FindStringSubmatch(html)
	if len(matches) < 2 {
		t.Fatal("licensePattern did not match")
	}
	if matches[1] != "MIT" {
		t.Errorf("expected MIT, got %q", matches[1])
	}
}

func TestExtractRepoFromGoGetMeta(t *testing.T) {
	html := `
<html><head>
<meta name="go-import" content="gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3">
<meta name="go-source" content="gopkg.in/yaml.v3 _ https://github.com/go-yaml/yaml/tree/v3.0.1{/dir} https://github.com/go-yaml/yaml/blob/v3.0.1{/dir}/{file}#L{line}">
</head></html>`
	got := extractRepoFromGoGetMeta(html)
	want := "https://github.com/go-yaml/yaml"
	if got != want {
		t.Fatalf("extractRepoFromGoGetMeta() = %q, want %q", got, want)
	}
}

func TestExtractRepoFromGoGetMeta_FallbackToGoImport(t *testing.T) {
	html := `<meta name="go-import" content="example.com/m git https://github.com/example/mod">`
	got := extractRepoFromGoGetMeta(html)
	want := "https://github.com/example/mod"
	if got != want {
		t.Fatalf("extractRepoFromGoGetMeta() = %q, want %q", got, want)
	}
}
