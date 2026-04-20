package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag/perm"
)

// pqtreeCommand creates the pqtree command for visualizing PQ-tree constraints.
func (c *CLI) pqtreeCommand() *cobra.Command {
	var output string
	var labels string

	cmd := &cobra.Command{
		Use:    "pqtree [constraints...]",
		Short:  "Experimental: render a PQ-tree with optional constraints",
		Hidden: true,
		Long: `Render a PQ-tree visualization showing valid permutations.

Constraints are comma-separated indices that must be adjacent.
Example: "0,1" means elements 0 and 1 must be adjacent.`,
		Example: `  # Universal tree with 4 elements
  stacktower pqtree --labels A,B,C,D -o tree.svg

  # With constraint: A,B must be adjacent  
  stacktower pqtree --labels A,B,C,D -o tree.svg 0,1

  # Multiple constraints
  stacktower pqtree --labels A,B,C,D -o tree.svg 0,1 2,3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			labelList := strings.Split(labels, ",")
			if len(labelList) == 0 {
				return NewUserError("at least one label required", "Use --labels A,B,C,D to specify labels.")
			}

			tree := perm.NewPQTree(len(labelList))

			for _, arg := range args {
				constraint, err := parseConstraint(arg)
				if err != nil {
					return WrapUserError(err, fmt.Sprintf("invalid constraint %q", arg), "Constraints are comma-separated indices, e.g., 0,1")
				}
				if !tree.Reduce(constraint) {
					return NewUserError(
						fmt.Sprintf("constraint %q made tree unsatisfiable", arg),
						"This constraint conflicts with previous constraints. Check for contradictions.",
					)
				}
			}

			svg, err := tree.RenderSVG(labelList)
			if err != nil {
				return WrapSystemError(err, "failed to render PQ-tree", "")
			}

			if err := writeFile(svg, output); err != nil {
				return WrapSystemError(err, "failed to write output", "Check that the output path is writable.")
			}

			ui.PrintSuccess("PQ-tree generated")
			if output != "" {
				ui.PrintFile(output)
			}
			ui.PrintKeyValue("Tree", tree.StringWithLabels(labelList))
			ui.PrintKeyValue("Permutations", fmt.Sprintf("%d", tree.ValidCount()))

			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (stdout if empty)")
	cmd.Flags().StringVar(&labels, "labels", "A,B,C,D", "comma-separated node labels")

	return cmd
}

// parseConstraint parses a constraint string like "0,1,2" into a slice of indices.
func parseConstraint(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return nil, fmt.Errorf("need at least 2 indices")
	}
	result := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("invalid index %q", p)
		}
		result[i] = n
	}
	return result, nil
}
