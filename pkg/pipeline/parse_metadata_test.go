package pipeline

import (
	"context"
	"testing"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestBuildResolveOptions_GitHubProviderWithoutToken(t *testing.T) {
	// GitHub enrichment should work even without a token (using unauthenticated API)
	t.Setenv("GITHUB_TOKEN", "")
	opts := buildResolveOptions(context.Background(), cache.NewNullCache(), Options{
		SkipEnrich: false,
	})
	if len(opts.MetadataProviders) != 1 {
		t.Fatalf("expected github provider even without token, got %d providers", len(opts.MetadataProviders))
	}
	if opts.MetadataProviders[0].Name() != "github" {
		t.Fatalf("provider = %q, want github", opts.MetadataProviders[0].Name())
	}
}

func TestBuildResolveOptions_RegistersGitHubWhenTokenProvided(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	opts := buildResolveOptions(context.Background(), cache.NewNullCache(), Options{
		SkipEnrich:  false,
		GitHubToken: "token",
	})
	if len(opts.MetadataProviders) != 1 {
		t.Fatalf("expected github provider only, got %d", len(opts.MetadataProviders))
	}
	if opts.MetadataProviders[0].Name() != "github" {
		t.Fatalf("provider = %q, want github", opts.MetadataProviders[0].Name())
	}
}

func TestBuildResolveOptions_SkipEnrichDisablesProviders(t *testing.T) {
	opts := buildResolveOptions(context.Background(), cache.NewNullCache(), Options{
		SkipEnrich: true,
	})
	if len(opts.MetadataProviders) != 0 {
		t.Fatalf("expected no metadata providers, got %d", len(opts.MetadataProviders))
	}
}

func TestBuildResolveOptions_DependencyScope(t *testing.T) {
	opts := buildResolveOptions(context.Background(), cache.NewNullCache(), Options{
		DependencyScope: deps.DependencyScopeAll,
	})
	if opts.DependencyScope != deps.DependencyScopeAll {
		t.Fatalf("dependency scope = %q, want %q", opts.DependencyScope, deps.DependencyScopeAll)
	}
}
