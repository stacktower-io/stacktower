package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
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
				_ = cmd.Help()
				return NewUserError(
					"missing required argument: <file> or language subcommand",
					"Try: stacktower parse poetry.lock, or: stacktower parse python requests",
				)
			}
			return c.runParseAutoDetect(cmd.Context(), &flags, args[0])
		},
	}

	cmd.PersistentFlags().IntVar(&flags.MaxDepth, "max-depth", flags.MaxDepth, "maximum dependency depth")
	cmd.PersistentFlags().IntVar(&flags.MaxNodes, "max-nodes", flags.MaxNodes, "maximum nodes to fetch")
	cmd.PersistentFlags().IntVar(&flags.Workers, "workers", flags.Workers, "concurrent fetch workers (default 20)")
	cmd.PersistentFlags().BoolVar(&flags.enrich, "enrich", true, "enrich with GitHub metadata (stars, maintainers)")
	cmd.PersistentFlags().BoolVar(&flags.FetchContributors, "contributors", false, "fetch GitHub contributors for Nebraska rankings (slower)")
	cmd.PersistentFlags().StringVar(&flags.DependencyScope, "dependency-scope", deps.DependencyScopeProdOnly, "dependency scope: prod_only or all")
	cmd.PersistentFlags().BoolVar(&flags.IncludePrerelease, "include-prerelease", false, "include prerelease versions (alpha/beta/rc/dev/etc.)")
	cmd.PersistentFlags().StringVar(&flags.RuntimeVersion, "runtime-version", "", "target runtime version for marker evaluation (e.g., '3.11' for Python)")
	cmd.PersistentFlags().StringVarP(&flags.output, "output", "o", "", "output file (default: TTY summary/tree, piped stdout emits JSON)")
	cmd.PersistentFlags().StringVarP(&flags.name, "name", "n", "", "project name (for manifest parsing)")
	cmd.PersistentFlags().BoolVar(&flags.noCache, "no-cache", false, "disable caching")
	cmd.PersistentFlags().BoolVar(&flags.scan, "security-scan", false, "best-effort scan for known vulnerabilities (OSV.dev)")

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

// runParse auto-detects whether arg is a manifest file or package name.
func (c *CLI) runParse(ctx context.Context, lang *deps.Language, flags *parseFlags, arg string) error {
	if lang.HasManifests() && looksLikeFile(arg) {
		return c.parseManifest(ctx, lang, flags, arg)
	}

	pkg, version := parsePackageVersion(arg)
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}
	if version != "" {
		flags.Version = version
	}

	return c.parsePackage(ctx, lang, flags, pkg)
}

// parsePackage parses a package using the pipeline service.
func (c *CLI) parsePackage(ctx context.Context, lang *deps.Language, flags *parseFlags, pkg string) error {
	start := time.Now()

	opts, displayName, err := buildRegistryParseOpts(flags.Options, lang, pkg, flags.enrich)
	if err != nil {
		return err
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

	opts, err := buildManifestParseOpts(flags.Options, lang, filePath, flags.enrich)
	if err != nil {
		return err
	}

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
		if err := result.Graph.RenameNode(graph.ProjectRootNodeID, name); err != nil {
			c.Logger.Debug("rename root node failed", "from", graph.ProjectRootNodeID, "to", name, "err", err)
		}
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
