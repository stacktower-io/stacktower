//go:build integration

package packagist

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
		{"laravel/framework", "laravel/framework", false},
		{"symfony/console", "symfony/console", false},
		{"nonexistent", "nonexistent-vendor/nonexistent-package-12345", true},
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

	pkg, err := client.FetchPackage(ctx, "symfony/console", false)
	if err != nil {
		t.Fatalf("FetchPackage(symfony/console) error: %v", err)
	}

	// symfony/console should have dependencies
	if len(pkg.Dependencies) == 0 {
		t.Error("symfony/console should have dependencies")
	}
}

func TestListVersions_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := client.ListVersions(ctx, "symfony/console", false)
	if err != nil {
		t.Fatalf("ListVersions(symfony/console) error: %v", err)
	}

	if len(versions) == 0 {
		t.Error("symfony/console should have versions")
	}
}
