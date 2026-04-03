//go:build integration

package rubygems

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetchGem_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		gem     string
		wantErr bool
	}{
		{"rails", "rails", false},
		{"sinatra", "sinatra", false},
		{"nonexistent", "this-gem-should-not-exist-12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gem, err := client.FetchGem(ctx, tt.gem, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("FetchGem(%q) error = %v, wantErr %v", tt.gem, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if gem.Name == "" {
					t.Error("gem name should not be empty")
				}
				if gem.Version == "" {
					t.Error("gem version should not be empty")
				}
			}
		})
	}
}

func TestFetchGemWithDeps_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gem, err := client.FetchGem(ctx, "rails", false)
	if err != nil {
		t.Fatalf("FetchGem(rails) error: %v", err)
	}

	// rails should have dependencies
	if len(gem.Dependencies) == 0 {
		t.Error("rails should have dependencies")
	}
}

func TestListVersions_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := client.ListVersions(ctx, "sinatra", false)
	if err != nil {
		t.Fatalf("ListVersions(sinatra) error: %v", err)
	}

	if len(versions) == 0 {
		t.Error("sinatra should have versions")
	}
}
