package maven

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

func TestParseCoordinate(t *testing.T) {
	tests := []struct {
		coord        string
		wantGroup    string
		wantArtifact string
		wantErr      bool
	}{
		{"org.springframework:spring-core", "org.springframework", "spring-core", false},
		{"com.google.guava:guava", "com.google.guava", "guava", false},
		{"invalid", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.coord, func(t *testing.T) {
			g, a, err := parseCoordinate(tt.coord)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCoordinate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if g != tt.wantGroup {
				t.Errorf("groupID = %v, want %v", g, tt.wantGroup)
			}
			if a != tt.wantArtifact {
				t.Errorf("artifactID = %v, want %v", a, tt.wantArtifact)
			}
		})
	}
}

func TestClient_FetchArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/solrsearch/select" {
			resp := searchResponse{}
			resp.Response.NumFound = 1
			resp.Response.Docs = []searchDoc{
				{GroupID: "org.example", ArtifactID: "mylib", LatestVersion: "1.0.0"},
			}
			json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/maven2/org/example/mylib/1.0.0/mylib-1.0.0.pom" {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(`<?xml version="1.0"?>
<project>
  <groupId>org.example</groupId>
  <artifactId>mylib</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.google.guava</groupId>
      <artifactId>guava</artifactId>
      <version>31.0</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server.URL+"/solrsearch/select")

	info, err := c.FetchArtifact(context.Background(), "org.example:mylib", true)
	if err != nil {
		t.Fatalf("FetchArtifact failed: %v", err)
	}

	if info.GroupID != "org.example" {
		t.Errorf("expected groupID org.example, got %s", info.GroupID)
	}
	if info.ArtifactID != "mylib" {
		t.Errorf("expected artifactID mylib, got %s", info.ArtifactID)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", info.Version)
	}
	if info.Coordinate() != "org.example:mylib" {
		t.Errorf("expected coordinate org.example:mylib, got %s", info.Coordinate())
	}
}

func TestClient_FetchArtifact_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := searchResponse{}
		resp.Response.NumFound = 0
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := testClient(t, server.URL)

	_, err := c.FetchArtifact(context.Background(), "org.missing:artifact", true)
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
}

func TestExtractDeps(t *testing.T) {
	pom := &pomProject{
		Dependencies: []pomDependency{
			{GroupID: "org.apache", ArtifactID: "commons-lang", Version: "3.12.0", Scope: "compile"},
			{GroupID: "junit", ArtifactID: "junit", Scope: "test"},
			{GroupID: "org.slf4j", ArtifactID: "slf4j-api", Scope: "provided"},
			{GroupID: "org.optional", ArtifactID: "opt", Optional: "true"},
			{GroupID: "${project.groupId}", ArtifactID: "internal"}, // property reference
		},
	}

	deps := extractDeps(pom)
	if len(deps) != 1 {
		t.Errorf("expected 1 dep, got %d: %v", len(deps), deps)
	}
	if deps[0].Name != "org.apache:commons-lang" {
		t.Errorf("expected org.apache:commons-lang, got %s", deps[0].Name)
	}
	if deps[0].Constraint != "3.12.0" {
		t.Errorf("expected constraint 3.12.0, got %s", deps[0].Constraint)
	}
}

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		Client:  integrations.NewClient(cache.NewNullCache(), "maven:", time.Hour, nil),
		baseURL: serverURL,
	}
}
