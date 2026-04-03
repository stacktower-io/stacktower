//go:build integration

package goproxy

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetchModule_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		module  string
		wantErr bool
	}{
		{"cobra", "github.com/spf13/cobra", false},
		{"gin", "github.com/gin-gonic/gin", false},
		{"nonexistent", "github.com/nonexistent/nonexistent-module-12345", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, err := client.FetchModule(ctx, tt.module, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("FetchModule(%q) error = %v, wantErr %v", tt.module, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if mod.Path == "" {
					t.Error("module path should not be empty")
				}
				if mod.Version == "" {
					t.Error("module version should not be empty")
				}
			}
		})
	}
}

func TestFetchModuleWithDeps_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mod, err := client.FetchModule(ctx, "github.com/spf13/cobra", false)
	if err != nil {
		t.Fatalf("FetchModule(cobra) error: %v", err)
	}

	// cobra should have dependencies
	if len(mod.Dependencies) == 0 {
		t.Error("cobra should have dependencies")
	}
}

func TestListVersions_Integration(t *testing.T) {
	client := NewClient(cache.NewNullCache(), time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	versions, err := client.ListVersions(ctx, "github.com/gin-gonic/gin", false)
	if err != nil {
		t.Fatalf("ListVersions(gin) error: %v", err)
	}

	if len(versions) == 0 {
		t.Error("gin should have versions")
	}
}
