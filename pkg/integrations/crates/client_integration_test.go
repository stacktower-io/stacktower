//go:build integration

package crates

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetchCrate_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		crate   string
		wantErr bool
	}{
		{"serde", "serde", false},
		{"tokio", "tokio", false},
		{"nonexistent", "this-crate-should-not-exist-12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := client.FetchCrate(ctx, tt.crate, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("FetchCrate(%q) error = %v, wantErr %v", tt.crate, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if info.Name == "" {
					t.Error("crate name should not be empty")
				}
				if info.Version == "" {
					t.Error("crate version should not be empty")
				}
			}
		})
	}
}

func TestFetchCrateWithDeps_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := client.FetchCrate(ctx, "serde", false)
	if err != nil {
		t.Fatalf("FetchCrate(serde) error: %v", err)
	}

	// serde should have dependencies
	if len(info.Dependencies) == 0 {
		t.Error("serde should have dependencies")
	}
}
