//go:build integration

package maven

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetchArtifact_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		coord   string
		wantErr bool
	}{
		{"guava", "com.google.guava:guava", false},
		{"junit", "junit:junit", false},
		{"nonexistent", "com.nonexistent:nonexistent-artifact-12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact, err := client.FetchArtifact(ctx, tt.coord, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("FetchArtifact(%q) error = %v, wantErr %v", tt.coord, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if artifact.GroupID == "" {
					t.Error("artifact groupId should not be empty")
				}
				if artifact.ArtifactID == "" {
					t.Error("artifact artifactId should not be empty")
				}
				if artifact.Version == "" {
					t.Error("artifact version should not be empty")
				}
			}
		})
	}
}

func TestFetchArtifactWithDeps_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	artifact, err := client.FetchArtifact(ctx, "com.google.guava:guava", false)
	if err != nil {
		t.Fatalf("FetchArtifact(guava) error: %v", err)
	}

	// guava should have dependencies
	if len(artifact.Dependencies) == 0 {
		t.Error("guava should have dependencies")
	}
}

func TestListVersions_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := client.ListVersions(ctx, "junit:junit", false)
	if err != nil {
		t.Fatalf("ListVersions(junit) error: %v", err)
	}

	if len(versions) == 0 {
		t.Error("junit should have versions")
	}
}
