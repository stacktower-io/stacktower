package cli

import (
	"encoding/json"
	"fmt"
	"os"

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

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (stdout if empty)")
	cmd.Flags().IntVar(&maxPaths, "max-paths", 10, "Maximum paths to display per target")
	cmd.Flags().BoolVar(&shortest, "shortest", false, "Show only the shortest path(s)")

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
		return WrapSystemError(err, "failed to load graph", "")
	}

	roots := ui.FindRoots(g)
	if len(roots) == 0 {
		return NewUserError("graph has no root nodes", "")
	}
	root := roots[0]

	w := os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return WrapSystemError(err, "failed to create output file", "")
		}
		defer f.Close()
		w = f
	}

	for i, target := range targets {
		if _, ok := g.Node(target); !ok {
			return NewUserError(
				fmt.Sprintf("package %q not found in the graph", target),
				fmt.Sprintf("Run `stacktower resolve %s` to see all packages.", input),
			)
		}

		var paths [][]string
		if shortest {
			paths = dag.ShortestPaths(g, root, target)
		} else {
			paths = dag.FindPaths(g, root, target, maxPaths)
		}

		depth := dag.ShortestDepth(paths)

		version := nodeVersion(g, target)

		switch format {
		case "json":
			result := whyResult{
				Target:       target,
				Version:      version,
				Paths:        paths,
				ShortestPath: depth,
				TotalPaths:   len(paths),
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return WrapSystemError(err, "failed to write JSON output", "")
			}
		default:
			if i > 0 {
				fmt.Fprintln(w)
			}
			ui.WritePaths(w, target, version, paths, depth)
		}
	}

	return nil
}

func nodeVersion(g *dag.DAG, id string) string {
	n, ok := g.Node(id)
	if !ok || n.Meta == nil {
		return ""
	}
	v, _ := n.Meta["version"].(string)
	return v
}
