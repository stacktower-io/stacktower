package cli

import (
	"os"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/graph"
)

// loadGraph reads a dependency graph from a file path or stdin (when input is "-").
// This is the shared entry point used by why, stats, diff, sbom, and render.
func loadGraph(input string) (*dag.DAG, error) {
	if input == "-" {
		return graph.ReadGraph(os.Stdin)
	}
	return graph.ReadGraphFile(input)
}
