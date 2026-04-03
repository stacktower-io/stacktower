package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/integrations"
	"github.com/matzehuels/stacktower/pkg/pipeline"
	"github.com/matzehuels/stacktower/pkg/session"
)

// parseFlags holds parse command options.
type parseFlags struct {
	pipeline.Options
	output  string
	noCache bool
	name    string // project name override for manifest parsing
	scan    bool   // run vulnerability scan after parsing
	enrich  bool   // enrich with GitHub metadata (default true for parse)
}

// parseCommand creates the parse command with language-specific subcommands.
func (c *CLI) parseCommand() *cobra.Command {
	flags := parseFlags{
		Options: pipeline.Options{
			MaxDepth: pipeline.DefaultMaxDepth,
			MaxNodes: pipeline.DefaultMaxNodes,
		},
	}

	cmd := &cobra.Command{
		Use:   "parse [file]",
		Short: "Parse dependency graphs from package managers or manifest files",
		Long: `Parse dependency graphs from package managers or local manifest files.

The command auto-detects the language from manifest filenames when given a file path.
Use language subcommands (e.g., 'parse python') to parse packages by name.
Results are cached locally for faster subsequent runs.

Examples:
  stacktower parse poetry.lock                            # Auto-detect language from file
  stacktower parse package.json                           # Auto-detect JavaScript
  stacktower parse python requests                        # Package from PyPI
  stacktower parse python poetry.lock                     # Explicit language + file
  stacktower parse python requests --no-cache             # Disable caching`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return c.runParseAutoDetect(cmd.Context(), &flags, args[0])
		},
	}

	cmd.PersistentFlags().IntVar(&flags.MaxDepth, "max-depth", flags.MaxDepth, "maximum dependency depth")
	cmd.PersistentFlags().IntVar(&flags.MaxNodes, "max-nodes", flags.MaxNodes, "maximum nodes to fetch")
	cmd.PersistentFlags().IntVar(&flags.Workers, "workers", flags.Workers, "concurrent fetch workers (default 20)")
	cmd.PersistentFlags().BoolVar(&flags.enrich, "enrich", true, "enrich with GitHub metadata (stars, maintainers)")
	cmd.PersistentFlags().BoolVar(&flags.FetchContributors, "contributors", false, "fetch GitHub contributors for Nebraska rankings (slower)")
	cmd.PersistentFlags().StringVar(&flags.DependencyScope, "dependency-scope", "prod_only", "dependency scope: prod_only or all")
	cmd.PersistentFlags().BoolVar(&flags.IncludePrerelease, "include-prerelease", false, "include prerelease versions (alpha/beta/rc/dev/etc.)")
	cmd.PersistentFlags().StringVar(&flags.RuntimeVersion, "runtime-version", "", "target runtime version for marker evaluation (e.g., '3.11' for Python)")
	cmd.PersistentFlags().StringVarP(&flags.output, "output", "o", "", "output file (stdout if empty)")
	cmd.PersistentFlags().StringVarP(&flags.name, "name", "n", "", "project name (for manifest parsing)")
	cmd.PersistentFlags().BoolVar(&flags.noCache, "no-cache", false, "disable caching")
	cmd.PersistentFlags().BoolVar(&flags.scan, "security-scan", false, "scan dependencies for known vulnerabilities (OSV.dev)")

	for _, lang := range languages.All {
		cmd.AddCommand(c.langCommand(lang, &flags))
	}

	cmd.AddCommand(c.parseGitHubCommand(&flags))

	return cmd
}

// langCommand creates a language-specific parse subcommand.
func (c *CLI) langCommand(lang *deps.Language, flags *parseFlags) *cobra.Command {
	return &cobra.Command{
		Use:   fmt.Sprintf("%s <package-or-file>", lang.Name),
		Short: fmt.Sprintf("Parse %s dependencies", lang.Name),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runParse(cmd.Context(), lang, flags, args[0])
		},
	}
}

// runParseAutoDetect detects the language from a manifest file and parses it.
func (c *CLI) runParseAutoDetect(ctx context.Context, flags *parseFlags, path string) error {
	if !looksLikeFile(path) {
		return NewUserError(
			fmt.Sprintf("cannot auto-detect language for %q (not a manifest file)", path),
			fmt.Sprintf("Use a language subcommand for packages: stacktower parse python %s", path),
		)
	}

	// Look up language from manifest filename
	manifestMap := deps.SupportedManifests(languages.All)
	filename := filepath.Base(path)
	langName, ok := manifestMap[filename]
	if !ok {
		return NewUserError(
			fmt.Sprintf("unsupported manifest file: %s", filename),
			fmt.Sprintf("Supported manifests: %s", formatSupportedManifests(manifestMap)),
		)
	}

	lang := languages.Find(langName)
	if lang == nil {
		return NewSystemError(
			fmt.Sprintf("language %q not found", langName),
			"This is an internal error. Please report this issue.",
		)
	}

	return c.parseManifest(ctx, lang, flags, path)
}

// formatSupportedManifests formats manifest map for error messages, grouped by language.
func formatSupportedManifests(manifestMap map[string]string) string {
	byLang := make(map[string][]string)
	for filename, lang := range manifestMap {
		byLang[lang] = append(byLang[lang], filename)
	}

	var parts []string
	for _, lang := range []string{"python", "javascript", "go", "rust", "ruby", "php", "java"} {
		if files, ok := byLang[lang]; ok {
			slices.Sort(files)
			parts = append(parts, strings.Join(files, ", "))
		}
	}
	return strings.Join(parts, ", ")
}

// runParse auto-detects whether arg is a manifest file or package name.
func (c *CLI) runParse(ctx context.Context, lang *deps.Language, flags *parseFlags, arg string) error {
	if lang.HasManifests() && looksLikeFile(arg) {
		return c.parseManifest(ctx, lang, flags, arg)
	}

	// Extract version from package@version syntax
	pkg, version := parsePackageVersion(arg)
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}

	// Version from argument overrides flag
	if version != "" {
		flags.Version = version
	}

	return c.parsePackage(ctx, lang, flags, pkg)
}

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

// parsePackage parses a package using the pipeline service.
func (c *CLI) parsePackage(ctx context.Context, lang *deps.Language, flags *parseFlags, pkg string) error {
	start := time.Now()

	if err := validateFlags(flags.MaxDepth, flags.MaxNodes); err != nil {
		return err
	}
	if err := validatePackageName(pkg); err != nil {
		return WrapUserError(err, fmt.Sprintf("invalid package name %q", pkg), "Use a registry package identifier without path traversal or control characters.")
	}

	opts := flags.Options
	opts.Language = lang.Name
	opts.Package = pkg
	opts.SkipEnrich = !flags.enrich

	displayName := pkg
	if opts.Version != "" {
		displayName = fmt.Sprintf("%s@%s", pkg, opts.Version)
	}

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, flags.scan,
		fmt.Sprintf("Resolving %s/%s...", lang.Name, displayName), flags.MaxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("resolve %s/%s", lang.Name, displayName), err)
	}

	return finishParse(finishParseOpts{
		Graph:          result.Graph,
		Output:         flags.output,
		LangName:       lang.Name,
		Source:         displayName,
		CacheHit:       result.CacheHit,
		Elapsed:        time.Since(start),
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
	})
}

// parseManifest parses a manifest file using the pipeline service.
func (c *CLI) parseManifest(ctx context.Context, lang *deps.Language, flags *parseFlags, filePath string) error {
	start := time.Now()

	if err := validateFlags(flags.MaxDepth, flags.MaxNodes); err != nil {
		return err
	}

	manifestContent, err := os.ReadFile(filePath)
	if err != nil {
		return WrapUserError(err, "failed to read manifest file", "Check that the file path exists and is readable.")
	}

	opts := flags.Options
	opts.Language = lang.Name
	opts.Manifest = string(manifestContent)
	opts.ManifestFilename = filepath.Base(filePath)
	opts.ManifestPath = filePath
	opts.SkipEnrich = !flags.enrich

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, flags.scan,
		fmt.Sprintf("Parsing %s...", filepath.Base(filePath)), flags.MaxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("parse %s", filepath.Base(filePath)), err)
	}

	name := flags.name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	if name != "" {
		result.Graph.RenameNode(graph.ProjectRootNodeID, name) //nolint:errcheck // non-critical rename
	}

	return finishParse(finishParseOpts{
		Graph:          result.Graph,
		Output:         flags.output,
		LangName:       lang.Name,
		Source:         filepath.Base(filePath),
		CacheHit:       result.CacheHit,
		Elapsed:        time.Since(start),
		RuntimeVersion: result.RuntimeVersion,
		RuntimeSource:  result.RuntimeSource,
	})
}

// finishParseOpts contains options for finishParse output.
type finishParseOpts struct {
	Graph          *dag.DAG
	Output         string
	LangName       string
	Source         string
	CacheHit       bool
	Elapsed        time.Duration
	RuntimeVersion string
	RuntimeSource  string
}

// finishParse writes output and prints summary.
//
// Behavior depends on output target:
//   - "-o file": JSON to file, styled summary to terminal
//   - stdout is TTY: resolve table + dependency tree (shows resolution then enriched data)
//   - stdout is piped: clean JSON only (no summary, to avoid corrupting the stream)
func finishParse(opts finishParseOpts) error {
	g := opts.Graph
	output := opts.Output
	langName := opts.LangName
	source := opts.Source
	cacheHit := opts.CacheHit
	elapsed := opts.Elapsed

	isTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	// Always show resolve table + tree on TTY, regardless of output file
	if isTTY {
		// Show runtime version used for resolution
		if opts.RuntimeVersion != "" {
			runtimeInfo := fmt.Sprintf("Runtime: %s %s", langName, opts.RuntimeVersion)
			sourceLabel := ""
			switch opts.RuntimeSource {
			case "cli":
				sourceLabel = "(from --runtime-version)"
			case "manifest":
				sourceLabel = "(from manifest)"
			case "package":
				sourceLabel = "(from package)"
			case "default":
				sourceLabel = "(default)"
			}
			if sourceLabel != "" {
				runtimeInfo += " " + ui.StyleDim.Render(sourceLabel)
			}
			fmt.Fprintln(os.Stderr, ui.StyleInfo.Render(runtimeInfo))
			fmt.Fprintln(os.Stderr)
		}

		roots := ui.FindRoots(g)
		rootID := ""
		if len(roots) > 0 {
			rootID = roots[0]
		}

		// First: show resolve table (constraints → pinned versions)
		result := pipeline.BuildResolveResult(g, rootID)
		ui.WriteResolveOutput(os.Stdout, result, true)
		fmt.Println()
		ui.PrintResolveSummary(os.Stdout, result)

		// Separator between resolve and tree views
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, ui.StyleDim.Render("─── Dependency Tree ───"))
		fmt.Fprintln(os.Stderr)

		// Second: show dependency tree (enriched metadata)
		stats := ui.WriteTree(os.Stdout, g, roots, ui.TreeOpts{Color: true, ShowMeta: true})
		fmt.Println()
		ui.PrintTreeSummary(os.Stdout, g.NodeCount(), stats)
		ui.PrintNewline()
	}

	if output != "" {
		if err := graph.WriteGraphFile(g, output); err != nil {
			return WrapSystemError(err, "failed to write output file", "Check that the output path is writable.")
		}

		ui.PrintSuccess("Resolved %s %s",
			ui.StyleHighlight.Render(source),
			ui.StyleDim.Render("("+langName+")"))
		ui.PrintFile(output)
		if !isTTY {
			depth := ui.GraphDepth(g)
			ui.PrintStats(g.NodeCount(), g.EdgeCount(), depth, cacheHit, elapsed)
		}
		ui.PrintNewline()
		ui.PrintNextStep("Render", "stacktower render "+output)
		return nil
	}

	if !isTTY {
		return graph.WriteGraph(g, os.Stdout)
	}

	ui.PrintNextStep("Save as JSON", fmt.Sprintf("stacktower parse %s %s -o deps.json", langName, source))
	return nil
}

// looksLikeFile returns true if arg appears to be a file path.
func looksLikeFile(arg string) bool {
	if _, err := os.Stat(arg); err == nil {
		return true
	}
	base := filepath.Base(arg)
	return deps.IsManifestSupported(base, languages.All)
}

// getGitHubToken returns the GitHub token from environment or stored session.
// Priority: GITHUB_TOKEN env var > stored CLI session > empty string.
func getGitHubToken(ctx context.Context) string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	// Try to load from stored session (from 'stacktower github login')
	store, err := session.NewCLIStore()
	if err != nil {
		return ""
	}

	sess, err := store.GetSession(ctx)
	if err != nil || sess == nil || sess.IsExpired() {
		return ""
	}

	return sess.AccessToken
}

// validateFlags validates common flag values.
func validateFlags(maxDepth, maxNodes int) error {
	if maxDepth < 1 {
		return NewUserError(
			"invalid max-depth value",
			"max-depth must be at least 1",
		)
	}
	if maxDepth > 100 {
		return NewUserError(
			"max-depth too large",
			"max-depth cannot exceed 100 to prevent excessive traversal",
		)
	}
	if maxNodes < 1 {
		return NewUserError(
			"invalid max-nodes value",
			"max-nodes must be at least 1",
		)
	}
	if maxNodes > 50000 {
		return NewUserError(
			"max-nodes too large",
			"max-nodes cannot exceed 50000 to prevent memory issues",
		)
	}
	return nil
}

// validatePackageName performs basic security validation on package names.
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("package name cannot be empty")
	}
	if len(name) > 256 {
		return fmt.Errorf("package name too long (max 256 characters)")
	}

	dangerousPatterns := []string{"..", "//", "\x00", "\\"}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(name, pattern) {
			return fmt.Errorf("invalid package name: contains %q", pattern)
		}
	}

	for _, r := range name {
		if r < 32 || r == 127 {
			return fmt.Errorf("invalid package name: contains control characters")
		}
	}

	return nil
}

func wrapParseFailure(operation string, err error) error {
	// Check for diamond dependency error first (npm-specific)
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
		return WrapSystemError(err, operation+" failed", "Package not found. Check the package name and spelling.")
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
