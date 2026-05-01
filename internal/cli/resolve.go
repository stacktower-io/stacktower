package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

// resolveFlags holds resolve command options. It embeds pipeline.Options
// so the shared helpers in parse.go (buildRegistryParseOpts /
// buildManifestParseOpts) can populate them the same way parse does.
type resolveFlags struct {
	pipeline.Options
	output  string
	name    string
	noCache bool
	enrich  bool
}

// resolveCommand creates the resolve command for quick dependency resolution testing.
func (c *CLI) resolveCommand() *cobra.Command {
	flags := resolveFlags{
		Options: pipeline.Options{
			MaxDepth:        pipeline.DefaultMaxDepth,
			MaxNodes:        pipeline.DefaultMaxNodes,
			DependencyScope: deps.DependencyScopeProdOnly,
		},
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

	cmd.Flags().IntVar(&flags.MaxDepth, "max-depth", flags.MaxDepth, "maximum dependency depth")
	cmd.Flags().IntVar(&flags.MaxNodes, "max-nodes", flags.MaxNodes, "maximum nodes to fetch")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "write JSON to file (resolve tree still prints to stdout)")
	cmd.Flags().StringVarP(&flags.name, "name", "n", "", "project name (for manifest parsing)")
	cmd.Flags().BoolVar(&flags.noCache, "no-cache", false, "disable caching")
	cmd.Flags().BoolVar(&flags.enrich, "enrich", false, "enrich with GitHub metadata (off by default)")
	cmd.Flags().StringVar(&flags.DependencyScope, "dependency-scope", flags.DependencyScope, "dependency scope: prod_only or all")
	cmd.Flags().BoolVar(&flags.IncludePrerelease, "include-prerelease", false, "include prerelease versions (alpha/beta/rc/dev/etc.)")
	cmd.Flags().StringVar(&flags.RuntimeVersion, "runtime-version", "", "target runtime version for marker evaluation (e.g., '3.11' for Python)")

	return cmd
}

func (c *CLI) runResolve(ctx context.Context, flags *resolveFlags, args []string) error {
	if len(args) == 2 {
		return c.resolveWith(ctx, flags, args[0], args[1])
	}
	arg := args[0]

	if looksLikeFile(arg) {
		filename := filepath.Base(arg)
		langName := deps.GetManifestLanguage(filename, languages.All)
		if langName == "" {
			return NewUserError(
				fmt.Sprintf("unrecognized manifest file: %s", filename),
				fmt.Sprintf("Use a supported manifest filename (%s) or run `stacktower resolve <language> <package>`.", ui.SupportedManifestList(languages.All)),
			)
		}
		return c.resolveWith(ctx, flags, langName, arg)
	}

	return NewUserError(
		fmt.Sprintf("cannot auto-detect language for %q", arg),
		fmt.Sprintf("Use `stacktower resolve <language> %s` for registry lookups, or a supported manifest filename (%s).", arg, ui.SupportedManifestList(languages.All)),
	)
}

// resolveWith handles both registry and manifest resolution. When arg is a
// manifest file on disk, it reads the file and renames the root node;
// otherwise it treats arg as a package name for a registry lookup.
func (c *CLI) resolveWith(ctx context.Context, flags *resolveFlags, langName, arg string) error {
	lang := languages.Find(langName)
	if lang == nil {
		return NewUserError(
			fmt.Sprintf("unsupported language: %s", langName),
			"Run `stacktower info` to list supported ecosystems.",
		)
	}

	var (
		opts    pipeline.Options
		source  string
		opLabel string
	)

	if lang.HasManifests() && looksLikeFile(arg) {
		var err error
		opts, err = buildManifestParseOpts(flags.Options, lang, arg, flags.enrich)
		if err != nil {
			return err
		}
		source = filepath.Base(arg)
		opLabel = fmt.Sprintf("Resolving %s...", source)
	} else {
		var displayName string
		var err error
		opts, displayName, err = buildRegistryParseOpts(flags.Options, lang, arg, flags.enrich)
		if err != nil {
			return err
		}
		source = displayName
		opLabel = fmt.Sprintf("Resolving %s/%s...", lang.Name, displayName)
	}

	result, err := c.runParseWithProgress(ctx, opts, flags.noCache, false, opLabel, flags.MaxNodes)
	if err != nil {
		return wrapParseFailure(fmt.Sprintf("resolve %s", source), err)
	}

	if opts.Manifest != "" {
		name := flags.name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
		}
		if name != "" {
			if err := result.Graph.RenameNode(graph.ProjectRootNodeID, name); err != nil {
				c.Logger.Debug("rename root node failed", "from", graph.ProjectRootNodeID, "to", name, "err", err)
			}
		}
	}

	return c.outputResolveResult(result, flags, lang.Name, source)
}

func (c *CLI) outputResolveResult(result *parseResult, flags *resolveFlags, langName, source string) error {
	g := result.Graph

	roots := ui.FindRoots(g)
	rootID := ""
	if len(roots) > 0 {
		rootID = roots[0]
	}

	resolveResult := pipeline.BuildResolveResult(g, rootID)

	ui.PrintRuntimeInfo(langName, result.RuntimeVersion, result.RuntimeSource)

	ui.WriteResolveOutput(os.Stdout, resolveResult, true)
	fmt.Println()
	ui.PrintResolveSummary(os.Stdout, resolveResult)

	if flags.output != "" {
		meta := pipeline.ResolveMetaJSON{
			RuntimeVersion:    result.RuntimeVersion,
			RuntimeSource:     result.RuntimeSource,
			DependencyScope:   flags.DependencyScope,
			IncludePrerelease: flags.IncludePrerelease,
		}
		jsonData := resolveResult.ToJSON(meta)

		if err := writeFormatted(flags.output, FormatJSON, map[string]func(io.Writer) error{
			FormatJSON: func(w io.Writer) error { return encodeJSON(w, jsonData) },
		}); err != nil {
			return err
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
