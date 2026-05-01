package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func (c *CLI) whyCommand() *cobra.Command {
	var (
		format   string
		output   string
		maxPaths int
		shortest bool
	)

	cmd := &cobra.Command{
		Use:   "why [graph.json|-] <package> [package...]",
		Short: "Show why a package is in the dependency tree",
		Long: `Find and display all dependency paths from the root to one or more target packages.

Answers the question "why is this package in my dependency tree?" by tracing
all paths from the root package to the specified target(s).`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runWhy(args[0], args[1:], format, output, maxPaths, shortest)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", FormatText, "output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (stdout if omitted)")
	cmd.Flags().IntVar(&maxPaths, "max-paths", 10, "maximum paths to display per target")
	cmd.Flags().BoolVar(&shortest, "shortest", false, "show only the shortest path(s)")

	return cmd
}

type whyResult struct {
	Target       string     `json:"target"`
	Version      string     `json:"version"`
	Paths        [][]string `json:"paths"`
	ShortestPath int        `json:"shortest_depth"`
	TotalPaths   int        `json:"total_paths"`
}

func (c *CLI) runWhy(input string, targets []string, format, output string, maxPaths int, shortest bool) error {
	g, err := loadGraph(input)
	if err != nil {
		return WrapSystemError(err, "failed to load graph", "Check that the file exists and contains valid graph JSON.")
	}

	roots := ui.FindRoots(g)
	if len(roots) == 0 {
		return NewUserError("graph has no root nodes", "")
	}
	root := roots[0]

	var missing []string
	for _, target := range targets {
		if _, ok := g.Node(target); !ok {
			missing = append(missing, target)
		}
	}
	if len(missing) > 0 {
		hint := missingPackageHint(g, missing[0])
		if len(missing) == 1 {
			return NewUserError(
				fmt.Sprintf("package %q not found in the graph", missing[0]),
				hint,
			)
		}
		return NewUserError(
			fmt.Sprintf("packages not found in the graph: %s", strings.Join(missing, ", ")),
			hint,
		)
	}

	results := make([]whyResult, 0, len(targets))
	for _, target := range targets {
		var paths [][]string
		if shortest {
			paths = dag.ShortestPaths(g, root, target)
		} else {
			paths = dag.FindPaths(g, root, target, maxPaths)
		}

		results = append(results, whyResult{
			Target:       target,
			Version:      ui.NodeVersion(g, target),
			Paths:        paths,
			ShortestPath: dag.ShortestDepth(paths),
			TotalPaths:   len(paths),
		})
	}

	writers := map[string]func(io.Writer) error{
		FormatJSON: func(w io.Writer) error {
			// Emit a single JSON object so consumers can parse the
			// output with one `json.Unmarshal` call (NDJSON streams
			// required bespoke decoding and broke `jq` in the common
			// case of multiple targets).
			return encodeJSON(w, struct {
				Results []whyResult `json:"results"`
			}{Results: results})
		},
		FormatText: func(w io.Writer) error {
			for i, r := range results {
				if i > 0 {
					fmt.Fprintln(w)
				}
				ui.WritePaths(w, r.Target, r.Version, r.Paths, r.ShortestPath)
			}
			return nil
		},
	}
	if err := writeFormatted(output, format, writers); err != nil {
		return err
	}

	if output != "" {
		ui.PrintNewline()
		ui.PrintSuccess("Paths written")
		ui.PrintFile(output)
	}
	return nil
}

// missingPackageHint returns an actionable hint when the requested target is
// not present in the graph. It prefers a substring match over the node IDs so
// typos surface useful candidates; otherwise it falls back to advising `stats`,
// which can enumerate the graph contents without re-resolving.
func missingPackageHint(g *dag.DAG, target string) string {
	suggestions := suggestPackages(g, target, 5)
	if len(suggestions) > 0 {
		return "Did you mean: " + strings.Join(suggestions, ", ") + "?"
	}
	return "Run `stacktower stats <graph>` to inspect the graph contents."
}

// suggestPackages returns up to `limit` node IDs that contain `target` as a
// case-insensitive substring. Results are stable-sorted so output is
// reproducible across runs.
func suggestPackages(g *dag.DAG, target string, limit int) []string {
	if g == nil || target == "" || limit <= 0 {
		return nil
	}
	needle := strings.ToLower(target)
	var matches []string
	for _, n := range g.Nodes() {
		if n.IsSynthetic() {
			continue
		}
		if strings.Contains(strings.ToLower(n.ID), needle) {
			matches = append(matches, n.ID)
		}
	}
	sort.Strings(matches)
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}
