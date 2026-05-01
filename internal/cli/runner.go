package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
	"github.com/stacktower-io/stacktower/pkg/security"
)

// =============================================================================
// Runner Factory
// =============================================================================

// newRunner creates a pipeline runner for CLI use.
// When securityScan is true, an OSV-backed vulnerability scanner is attached.
func (c *CLI) newRunner(noCache bool, securityScan bool) (*pipeline.Runner, error) {
	cc, err := newCache(noCache)
	if err != nil {
		return nil, err
	}

	var scanner security.Scanner
	if securityScan {
		scanner = security.NewOSVScanner(nil) // default HTTP client
	}

	return pipeline.NewRunnerWithScanner(cc, nil, c.Logger, scanner), nil
}

func newCache(noCache bool) (cache.Cache, error) {
	if noCache {
		return cache.NewNullCache(), nil
	}
	dir, err := cacheDir()
	if err != nil {
		// No home directory and no XDG_CACHE_HOME. Fall back to an in-memory
		// NullCache so the command still works, but let the user know they
		// are paying the full fetch cost on every invocation.
		ui.PrintWarning("Cache disabled: could not locate cache directory (%v)", err)
		ui.PrintDetail("Set XDG_CACHE_HOME or HOME to enable persistent caching.")
		return cache.NewNullCache(), nil
	}
	fc, err := cache.NewFileCache(dir)
	if err != nil {
		return nil, err
	}
	return cache.NewInstrumentedCache(fc), nil
}

// cacheDir returns the cache directory using XDG standard (~/.cache/stacktower/).
func cacheDir() (string, error) {
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return filepath.Join(cacheHome, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", appName), nil
}

// =============================================================================
// Shared Parse Pipeline
// =============================================================================

// parseResult holds the output of a parse pipeline run.
type parseResult struct {
	Graph          *dag.DAG
	CacheHit       bool
	RuntimeVersion string // Target runtime version used (e.g., "3.11")
	RuntimeSource  string // Where runtime came from: "cli", "manifest", "default"
}

// runParseWithProgress creates a runner, starts a progress view, runs ParseWithCacheInfo,
// and stops the progress view. This is the shared entrypoint used by both `parse` and `resolve`.
func (c *CLI) runParseWithProgress(ctx context.Context, opts pipeline.Options, noCache, securityScan bool, progressMsg string, maxNodes int) (*parseResult, error) {
	runner, err := c.newRunner(noCache, securityScan)
	if err != nil {
		return nil, WrapSystemError(err, "failed to initialize pipeline runner",
			"This is usually a cache directory problem. Try --no-cache to bypass the local cache.")
	}
	defer runner.Close()

	opts.GitHubToken = c.getGitHubToken(ctx)
	opts.SecurityScan = securityScan

	// Warn about slower parsing when fetching contributors
	if opts.FetchContributors {
		ui.PrintInfo("Fetching GitHub contributors (this may be slower)")
	}

	pv := ui.NewProgressView(ctx, progressMsg, maxNodes)

	pvLogger := log.New(ui.NewProgressWriter(pv, os.Stderr))
	pvLogger.SetLevel(c.Logger.GetLevel())
	opts.Logger = pvLogger

	pv.Start()

	result, err := runner.ParseWithCacheInfo(ctx, opts)
	if err != nil {
		pv.StopWithError("Failed to resolve dependencies")
		return nil, err
	}
	pv.Stop()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return &parseResult{
		Graph:          result.Graph,
		CacheHit:       result.CacheHit,
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
	}, nil
}
