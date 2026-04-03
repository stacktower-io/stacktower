//go:build integration

package pypi

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetchPackage_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		pkg     string
		wantErr bool
	}{
		{"requests", "requests", false},
		{"flask", "flask", false},
		{"nonexistent", "this-package-should-not-exist-12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := client.FetchPackage(ctx, tt.pkg, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("FetchPackage(%q) error = %v, wantErr %v", tt.pkg, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if pkg.Name == "" {
					t.Error("package name should not be empty")
				}
				if pkg.Version == "" {
					t.Error("package version should not be empty")
				}
			}
		})
	}
}

func TestFetchPackageWithDeps_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pkg, err := client.FetchPackage(ctx, "requests", false)
	if err != nil {
		t.Fatalf("FetchPackage(requests) error: %v", err)
	}

	// requests should have some dependencies
	if len(pkg.Dependencies) == 0 {
		t.Error("requests should have dependencies")
	}
}
