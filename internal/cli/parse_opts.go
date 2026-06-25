package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/integrations"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
	"github.com/stacktower-io/stacktower/pkg/session"
)

// parsePackageVersion extracts package name and optional version from "package@version" syntax.
// Returns (package, version) where version is empty if not specified.
func parsePackageVersion(arg string) (string, string) {
	// Handle scoped packages like @scope/pkg@version
	if strings.HasPrefix(arg, "@") {
		// Find @ after the scope
		idx := strings.Index(arg[1:], "@")
		if idx != -1 {
			idx++ // Account for the leading @
			return arg[:idx], arg[idx+1:]
		}
		return arg, ""
	}

	// Regular package@version
	if idx := strings.LastIndex(arg, "@"); idx != -1 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, ""
}

// buildRegistryParseOpts validates the user-supplied limits and package
// name, then returns a pipeline.Options populated for a registry lookup
// along with a human-readable display name ("pkg" or "pkg@version").
//
// Shared by `parse <lang> <pkg>` and `resolve <lang> <pkg>` so both
// commands treat limits, name normalisation, and enrichment the same way.
func buildRegistryParseOpts(base pipeline.Options, lang *deps.Language, pkgArg string, enrich bool) (pipeline.Options, string, error) {
	if err := validateFlags(base.MaxDepth, base.MaxNodes); err != nil {
		return pipeline.Options{}, "", err
	}
	if err := validateDependencyScope(base.DependencyScope); err != nil {
		return pipeline.Options{}, "", err
	}

	pkg, version := parsePackageVersion(pkgArg)
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}
	if err := validatePackageName(pkg); err != nil {
		return pipeline.Options{}, "", err
	}

	opts := base
	opts.Language = lang.Name
	opts.Package = pkg
	if version != "" {
		opts.Version = version
	}
	opts.SkipEnrich = !enrich

	displayName := pkg
	if opts.Version != "" {
		displayName = fmt.Sprintf("%s@%s", pkg, opts.Version)
	}
	return opts, displayName, nil
}

// buildManifestParseOpts validates limits and reads the manifest file
// into memory, returning a pipeline.Options populated for manifest
// parsing. Shared between `parse` and `resolve` so both commands agree
// on where manifest bytes come from and how ManifestPath/ManifestFilename
// are populated.
func buildManifestParseOpts(base pipeline.Options, lang *deps.Language, filePath string, enrich bool) (pipeline.Options, error) {
	if err := validateFlags(base.MaxDepth, base.MaxNodes); err != nil {
		return pipeline.Options{}, err
	}
	if err := validateDependencyScope(base.DependencyScope); err != nil {
		return pipeline.Options{}, err
	}

	manifestContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pipeline.Options{}, WrapUserError(err,
				fmt.Sprintf("manifest file not found: %s", filePath),
				fmt.Sprintf("Check the path, or use `stacktower parse %s <package>` for a registry lookup.", lang.Name))
		}
		return pipeline.Options{}, WrapUserError(err, "failed to read manifest file", "Check that the file path exists and is readable.")
	}

	opts := base
	opts.Language = lang.Name
	opts.Manifest = string(manifestContent)
	opts.ManifestFilename = filepath.Base(filePath)
	opts.ManifestPath = filePath
	opts.SkipEnrich = !enrich
	return opts, nil
}

// looksLikeFile returns true if arg appears to be a file path.
//
// Recognized manifest filenames (e.g. "package.json") are routed to the
// manifest path even when the file doesn't exist, so the user gets a clear
// "manifest file not found" error instead of a nonsensical registry lookup
// for a filename.
func looksLikeFile(arg string) bool {
	if _, err := os.Stat(arg); err == nil {
		return true
	}
	base := filepath.Base(arg)
	return deps.IsManifestSupported(base, languages.All)
}

// formatSupportedManifests formats manifest map for error messages, grouped by language.
func formatSupportedManifests(manifestMap map[string]string) string {
	byLang := make(map[string][]string)
	for filename, lang := range manifestMap {
		byLang[lang] = append(byLang[lang], filename)
	}

	var parts []string
	seen := make(map[string]struct{}, len(byLang))
	for _, lang := range languages.All {
		if files, ok := byLang[lang.Name]; ok {
			slices.Sort(files)
			parts = append(parts, strings.Join(files, ", "))
			seen[lang.Name] = struct{}{}
		}
	}

	var extraLangs []string
	for lang := range byLang {
		if _, ok := seen[lang]; !ok {
			extraLangs = append(extraLangs, lang)
		}
	}
	slices.Sort(extraLangs)
	for _, lang := range extraLangs {
		if files, ok := byLang[lang]; ok {
			slices.Sort(files)
			parts = append(parts, strings.Join(files, ", "))
		}
	}
	return strings.Join(parts, ", ")
}

// getGitHubToken returns the GitHub token from environment or stored session.
// Priority: GITHUB_TOKEN env var > stored CLI session > empty string.
//
// Errors reading the local session store are intentionally non-fatal — they
// just mean we fall back to unauthenticated GitHub access — but we still log
// them at debug so that `-v` surfaces auth issues during troubleshooting.
func (c *CLI) getGitHubToken(ctx context.Context) string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	store, err := session.NewCLIStore()
	if err != nil {
		c.Logger.Debug("github session store unavailable", "err", err)
		return ""
	}

	sess, err := store.GetSession(ctx)
	if errors.Is(err, session.ErrExpired) {
		c.Logger.Debug("github session expired; falling back to unauthenticated access")
		// Surface a single-line user-visible hint (not just a debug log) so
		// it's obvious why a request that was authenticated yesterday is
		// suddenly hitting unauthenticated rate limits. We only print it
		// once per process to avoid repetition when a single command makes
		// multiple pipeline calls.
		expiredSessionHintOnce.Do(func() {
			ui.PrintWarning("GitHub session expired — run `stacktower github login` to re-authenticate.")
		})
		return ""
	}
	if err != nil {
		c.Logger.Debug("github session read failed", "err", err)
		return ""
	}
	if sess == nil {
		return ""
	}

	return sess.AccessToken
}

// expiredSessionHintOnce ensures the "session expired" warning is only
// printed at most once per process, even if getGitHubToken is called
// multiple times (e.g. parse + enrichment + rate-limit retries).
var expiredSessionHintOnce sync.Once

// validateFlags validates user-supplied --max-depth/--max-nodes values.
//
// The CLI pre-populates these flags with pipeline.DefaultMaxDepth and
// pipeline.DefaultMaxNodes before cobra parses args, so a zero value here
// unambiguously means the user typed `--max-depth=0` (which we reject as
// nonsense). Upper bounds are shared with the pipeline via
// pipeline.MaxAllowedDepth/MaxAllowedNodes so API and CLI agree on what
// counts as "too large". Errors are wrapped as CLIError(ErrorKindUser) to
// ensure the process exits with a usage-error code.
func validateFlags(maxDepth, maxNodes int) error {
	if maxDepth < 1 {
		return NewUserError(
			"invalid max-depth value",
			"max-depth must be at least 1",
		)
	}
	if maxDepth > pipeline.MaxAllowedDepth {
		return NewUserError(
			"max-depth too large",
			fmt.Sprintf("max-depth cannot exceed %d to prevent excessive traversal", pipeline.MaxAllowedDepth),
		)
	}
	if maxNodes < 1 {
		return NewUserError(
			"invalid max-nodes value",
			"max-nodes must be at least 1",
		)
	}
	if maxNodes > pipeline.MaxAllowedNodes {
		return NewUserError(
			"max-nodes too large",
			fmt.Sprintf("max-nodes cannot exceed %d to prevent memory issues", pipeline.MaxAllowedNodes),
		)
	}
	return nil
}

// validateDependencyScope validates the user-supplied --dependency-scope
// value, returning a user error (usage exit code) for unknown scopes instead
// of letting the pipeline reject it as a system error.
func validateDependencyScope(scope string) error {
	if scope == "" || scope == deps.DependencyScopeProdOnly || scope == deps.DependencyScopeAll {
		return nil
	}
	return NewUserError(
		fmt.Sprintf("invalid dependency-scope: %q", scope),
		fmt.Sprintf("Supported scopes: %s (default), %s.", deps.DependencyScopeProdOnly, deps.DependencyScopeAll),
	)
}

// validatePackageName performs a minimal sanity check on an argument that
// will be used as a registry package identifier.
//
// This is NOT a full ecosystem-aware name validator: language-specific rules
// (PEP 503 normalization, npm scopes, Maven coordinate shape, etc.) are
// handled downstream by each language's fetcher. The checks here are only
// intended to catch obviously-dangerous input before it reaches any I/O:
//
//   - empty / excessively long names
//   - path-traversal fragments ("..", "//", "\\")
//   - embedded NULs or other control characters
//
// Anything else is deferred to the language fetcher, which will return a
// clearer ecosystem-specific error if the package is malformed.
func validatePackageName(name string) error {
	const hint = "Use a registry package identifier without path traversal or control characters."

	if name == "" {
		return NewUserError("package name cannot be empty", hint)
	}
	if len(name) > 256 {
		return NewUserError("package name too long (max 256 characters)", hint)
	}

	dangerousPatterns := []string{"..", "//", "\x00", "\\"}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(name, pattern) {
			return NewUserError(fmt.Sprintf("invalid package name: contains %q", pattern), hint)
		}
	}

	for _, r := range name {
		if r < 32 || r == 127 {
			return NewUserError("invalid package name: contains control characters", hint)
		}
	}

	return nil
}

func wrapParseFailure(operation string, err error) error {
	var diamondErr *deps.DiamondDependencyError
	if errors.As(err, &diamondErr) {
		return WrapSystemError(diamondErr, operation+" failed",
			fmt.Sprintf(`Dependency conflict: %q requires incompatible versions.

npm allows multiple versions via nested node_modules, but this resolver
requires a single version per package.

Workaround: Use package-lock.json which has the pre-resolved tree:
  stacktower resolve package-lock.json`, diamondErr.Package))
	}

	switch {
	case errors.Is(err, integrations.ErrNotFound):
		return WrapUserError(err, operation+" failed", "Package not found. Check the package name and spelling.")
	case integrations.IsRateLimitedError(err):
		return WrapSystemError(err, operation+" failed", "Rate limit exceeded. Wait and retry, or configure GITHUB_TOKEN for higher limits.")
	case errors.Is(err, context.DeadlineExceeded):
		return WrapSystemError(err, operation+" timed out", "Retry with a longer timeout, fewer nodes, or with cache enabled.")
	case errors.Is(err, context.Canceled):
		return err
	default:
		return WrapSystemError(err, operation+" failed", "Re-run with --verbose for diagnostics and check network connectivity.")
	}
}
