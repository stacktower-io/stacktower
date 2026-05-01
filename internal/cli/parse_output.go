package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

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
	Ref            string // git ref (branch/tag) for GitHub-parsed packages
}

// finishParse writes output and prints summary. It dispatches to one of three
// per-mode helpers so each execution path is easy to read in isolation:
//
//   - writeParseOutputFile: user passed -o; write JSON to a file, print a
//     styled summary to the TTY (or a compact one to stderr when piped).
//   - writeParseOutputTTY:  stdout is a TTY and no -o; show the resolve
//     table and a human dependency tree, and hint at -o for saving.
//   - writeParseOutputPipe: stdout is not a TTY and no -o; emit only the
//     graph JSON so downstream tooling receives a clean stream.
func finishParse(opts finishParseOpts) error {
	isTTY := ui.StdoutIsTTY()

	switch {
	case opts.Output != "":
		return writeParseOutputFile(opts, isTTY)
	case isTTY:
		return writeParseOutputTTY(opts)
	default:
		return writeParseOutputPipe(opts)
	}
}

// writeParseOutputTTY prints a styled resolve table + dependency tree to
// stdout and hints at how to save the result as JSON. Used when stdout is a
// TTY and the user did not pass -o.
func writeParseOutputTTY(opts finishParseOpts) error {
	g := opts.Graph
	ui.PrintRuntimeInfo(opts.LangName, opts.RuntimeVersion, opts.RuntimeSource)

	roots := ui.FindRoots(g)
	rootID := ""
	if len(roots) > 0 {
		rootID = roots[0]
	}

	result := pipeline.BuildResolveResult(g, rootID)
	ui.WriteResolveOutput(os.Stdout, result, true)
	fmt.Println()
	ui.PrintResolveSummary(os.Stdout, result)

	ui.PrintSeparator("Dependency Tree")
	stats := ui.WriteTree(os.Stdout, g, roots, ui.TreeOpts{Color: true, ShowMeta: true})
	fmt.Println()
	ui.PrintTreeSummary(os.Stdout, g.NodeCount(), stats)
	ui.PrintNewline()

	suggested := suggestOutputName(g, opts.Ref)
	ui.PrintNextStep("Save as JSON", fmt.Sprintf("stacktower parse %s %s -o %s", opts.LangName, opts.Source, suggested))
	return nil
}

// writeParseOutputFile writes the full graph to opts.Output and prints a
// styled summary (TTY) or compact stats line (pipe) so the user still gets
// feedback. On a TTY we also show the resolve table but skip the full tree
// dump to keep the success message visible.
func writeParseOutputFile(opts finishParseOpts, isTTY bool) error {
	g := opts.Graph

	if isTTY {
		ui.PrintRuntimeInfo(opts.LangName, opts.RuntimeVersion, opts.RuntimeSource)
		roots := ui.FindRoots(g)
		rootID := ""
		if len(roots) > 0 {
			rootID = roots[0]
		}
		result := pipeline.BuildResolveResult(g, rootID)
		ui.WriteResolveOutput(os.Stdout, result, true)
		fmt.Println()
		ui.PrintResolveSummary(os.Stdout, result)
		ui.PrintNewline()
	}

	if err := graph.WriteGraphFile(g, opts.Output); err != nil {
		return WrapSystemError(err, "failed to write output file", "Check that the output path is writable.")
	}

	ui.PrintSuccess("Resolved %s %s",
		ui.StyleHighlight.Render(opts.Source),
		ui.StyleDim.Render("("+opts.LangName+")"))
	ui.PrintFile(opts.Output)
	if !isTTY {
		depth := dag.ComputeStats(g).MaxDepth
		ui.PrintStats(g.NodeCount(), g.EdgeCount(), depth, opts.CacheHit, opts.Elapsed)
	}
	ui.PrintNewline()
	ui.PrintNextStep("Render", "stacktower render "+opts.Output)
	return nil
}

// writeParseOutputPipe emits the graph as JSON on stdout with no
// decorations; anything else would corrupt the stream for consumers like
// `jq` or `stacktower stats -`.
func writeParseOutputPipe(opts finishParseOpts) error {
	return graph.WriteGraph(opts.Graph, os.Stdout)
}

// suggestOutputName builds a descriptive filename from the graph's root node.
// For registry packages it produces "flask-3.1.0.json"; for GitHub-parsed
// packages with a ref it produces "repo-v2.0.0.json". Falls back to
// "root.json" when no version/ref data is available.
func suggestOutputName(g *dag.DAG, ref string) string {
	roots := ui.FindRoots(g)
	if len(roots) == 0 {
		return "deps.json"
	}
	root := roots[0]

	if ref != "" {
		return root + "-" + sanitizeFilenameSegment(ref) + ".json"
	}

	if n, ok := g.Node(root); ok && n.Meta != nil {
		if v, ok := n.Meta["version"].(string); ok && v != "" {
			return root + "-" + sanitizeFilenameSegment(v) + ".json"
		}
	}

	return root + ".json"
}

// sanitizeFilenameSegment replaces filesystem-unfriendly characters in a
// version or ref string so it can safely appear in a filename.
func sanitizeFilenameSegment(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, s)
}
