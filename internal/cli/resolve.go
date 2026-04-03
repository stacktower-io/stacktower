package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/core/deps"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/pipeline"
)

type resolveFlags struct {
	maxDepth          int
	maxNodes          int
	output            string
	name              string
	noCache           bool
	enrich            bool
	dependencyScope   string
	includePrerelease bool
	runtimeVersion    string
}

// resolveCommand creates the resolve command for quick dependency resolution testing.
func (c *CLI) resolveCommand() *cobra.Command {
	flags := resolveFlags{
		maxDepth:          pipeline.DefaultMaxDepth,
		maxNodes:          pipeline.DefaultMaxNodes,
		dependencyScope:   deps.DependencyScopeProdOnly,
		includePrerelease: false,
	}

	cmd := &cobra.Command{
		Use:   "resolve <manifest-file | language package[@version]>",
		Short: "Resolve dependencies and print the dependency tree",
		Long: `Resolve dependencies from a manifest file or package registry and print
a human-readable dependency tree.

The language is auto-detected from the manifest filename, so you don't need
to specify it. For registry lookups, provide the language and package name.

Examples:
  stacktower resolve poetry.lock
  stacktower resolve Cargo.lock
  stacktower resolve package-lock.json
  stacktower resolve go.mod
  stacktower resolve python fastapi
  stacktower resolve rust serde@1.0.195
  stacktower resolve poetry.lock -o deps.json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runResolve(cmd.Context(), &flags, args)
		},
	}

	cmd.Flags().IntVar(&flags.maxDepth, "max-depth", flags.maxDepth, "maximum dependency depth")
	cmd.Flags().IntVar(&flags.maxNodes, "max-nodes", flags.maxNodes, "maximum nodes to fetch")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "output file (stdout if empty)")
	cmd.Flags().StringVarP(&flags.name, "name", "n", "", "project name (for manifest parsing)")
	cmd.Flags().BoolVar(&flags.noCache, "no-cache", false, "disable caching")
	cmd.Flags().BoolVar(&flags.enrich, "enrich", false, "enrich with GitHub metadata (off by default)")
	cmd.Flags().StringVar(&flags.dependencyScope, "dependency-scope", flags.dependencyScope, "dependency scope: prod_only or all")
	cmd.Flags().BoolVar(&flags.includePrerelease, "include-prerelease", false, "include prerelease versions (alpha/beta/rc/dev/etc.)")
	cmd.Flags().StringVar(&flags.runtimeVersion, "runtime-version", "", "target runtime version for marker evaluation (e.g., '3.11' for Python)")

	return cmd
}

func (c *CLI) runResolve(ctx context.Context, flags *resolveFlags, args []string) error {
	if len(args) == 2 {
		return c.resolveFromRegistry(ctx, flags, args[0], args[1])
	}
	arg := args[0]

	// Try auto-detecting language from filename
	filename := filepath.Base(arg)
	langName := deps.GetManifestLanguage(filename, languages.All)
	if langName != "" {
		return c.resolveManifest(ctx, flags, langName, arg)
	}

	// If the file exists on disk, try harder: maybe it's a path with a recognized basename
	if _, err := os.Stat(arg); err == nil {
		return NewUserError(
			fmt.Sprintf("unrecognized manifest file: %s", filename),
			fmt.Sprintf("Use a supported manifest filename (%s) or run `stacktower resolve <language> <package>`.", ui.SupportedManifestList(languages.All)),
		)
	}

	return NewUserError(
		fmt.Sprintf("cannot auto-detect language for %q", arg),
		fmt.Sprintf("Use `stacktower resolve <language> %s` for registry lookups, or a supported manifest filename (%s).", arg, ui.SupportedManifestList(languages.All)),
	)
}

func (c *CLI) resolveFromRegistry(ctx context.Context, flags *resolveFlags, langName, pkgArg string) error {
	start := time.Now()

	if err := validateFlags(flags.maxDepth, flags.maxNodes); err != nil {
		return err
	}

	lang := languages.Find(langName)
	if lang == nil {
		return NewUserError(
			fmt.Sprintf("unsupported language: %s", langName),
			"Run `stacktower info` to list supported ecosystems.",
		)
	}

	pkg, version := parsePackageVersion(pkgArg)
	if lang.NormalizeName != nil {
		pkg = lang.NormalizeName(pkg)
	}
	if err := validatePackageName(pkg); err != nil {
		return WrapUserError(err, fmt.Sprintf("invalid package name %q", pkg), "Use a valid registry package identifier.")
	}

	opts := pipeline.Options{
		Language:          lang.Name,
		Package:           pkg,
		Version:           version,
		MaxDepth:          flags.maxDepth,
		MaxNodes:          flags.maxNodes,
		SkipEnrich:        !flags.enrich,
		DependencyScope:   flags.dependencyScope,
		IncludePrerelease: flags.includePrerelease,
		RuntimeVersion:    flags.runtimeVersion,
	}

	displayName := pkg
	if version != "" {
		displayName = fmt.Sprintf("%s@%s", pkg, version)
	}

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, false,
		fmt.Sprintf("Resolving %s/%s...", lang.Name, displayName), flags.maxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("resolve %s/%s", lang.Name, displayName), err)
	}

	return c.outputResolveResult(result, flags, lang.Name, displayName, time.Since(start))
}

func (c *CLI) resolveManifest(ctx context.Context, flags *resolveFlags, langName, filePath string) error {
	start := time.Now()

	if err := validateFlags(flags.maxDepth, flags.maxNodes); err != nil {
		return err
	}

	lang := languages.Find(langName)
	if lang == nil {
		return NewUserError(
			fmt.Sprintf("unsupported language: %s", langName),
			"Run `stacktower info` to list supported ecosystems.",
		)
	}

	manifestContent, err := os.ReadFile(filePath)
	if err != nil {
		return WrapUserError(err, "failed to read manifest file", "Check that the file path exists and is readable.")
	}

	opts := pipeline.Options{
		Language:          lang.Name,
		Manifest:          string(manifestContent),
		ManifestFilename:  filepath.Base(filePath),
		ManifestPath:      filePath,
		MaxDepth:          flags.maxDepth,
		MaxNodes:          flags.maxNodes,
		SkipEnrich:        !flags.enrich,
		DependencyScope:   flags.dependencyScope,
		IncludePrerelease: flags.includePrerelease,
		RuntimeVersion:    flags.runtimeVersion,
	}

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, false,
		fmt.Sprintf("Resolving %s...", filepath.Base(filePath)), flags.maxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("resolve %s", filepath.Base(filePath)), err)
	}

	name := flags.name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}
	if name != "" {
		result.Graph.RenameNode(graph.ProjectRootNodeID, name) //nolint:errcheck // non-critical rename
	}

	return c.outputResolveResult(result, flags, langName, filepath.Base(filePath), time.Since(start))
}

func (c *CLI) outputResolveResult(result *parseResult, flags *resolveFlags, langName, source string, elapsed time.Duration) error {
	g := result.Graph

	// Find root node
	roots := ui.FindRoots(g)
	rootID := ""
	if len(roots) > 0 {
		rootID = roots[0]
	}

	// Build resolve result
	resolveResult := pipeline.BuildResolveResult(g, rootID)

	// Show runtime version used for resolution
	if result.RuntimeVersion != "" {
		runtimeInfo := fmt.Sprintf("Runtime: %s %s", langName, result.RuntimeVersion)
		sourceLabel := ""
		switch result.RuntimeSource {
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

	// Write text table to stdout
	ui.WriteResolveOutput(os.Stdout, resolveResult, true)

	fmt.Println()
	ui.PrintResolveSummary(os.Stdout, resolveResult)

	// Also output JSON if -o flag is set
	if flags.output != "" {
		meta := pipeline.ResolveMetaJSON{
			RuntimeVersion:    result.RuntimeVersion,
			RuntimeSource:     result.RuntimeSource,
			DependencyScope:   flags.dependencyScope,
			IncludePrerelease: flags.includePrerelease,
		}
		jsonData := resolveResult.ToJSON(meta)

		f, err := os.Create(flags.output)
		if err != nil {
			return WrapSystemError(err, "failed to create output file", "Check that the output path is writable.")
		}
		defer f.Close()

		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(jsonData); err != nil {
			return WrapSystemError(err, "failed to write JSON output", "Check that you have sufficient disk space.")
		}

		ui.PrintNewline()
		ui.PrintSuccess("Resolution written")
		ui.PrintFile(flags.output)
		ui.PrintNewline()
		ui.PrintNextStep("Full graph", fmt.Sprintf("stacktower parse %s %s -o graph.json", langName, source))
		return nil
	}

	ui.PrintNewline()
	ui.PrintNextStep("Save as JSON", fmt.Sprintf("stacktower resolve %s %s -o deps.json", langName, source))
	ui.PrintNextStep("Full graph", fmt.Sprintf("stacktower parse %s %s -o graph.json", langName, source))

	return nil
}
