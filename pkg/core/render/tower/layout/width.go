package layout

import (
	"math"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

const eps = 1e-9

// ComputeWidths assigns horizontal widths to nodes by distributing the
// frame width among top-level nodes and propagating that width down to
// children. This results in "top-heavy" towers where the root nodes are wide.
func ComputeWidths(g *dag.DAG, orders map[int][]string, frameWidth float64) map[string]float64 {
	rows := g.RowIDs()
	if len(rows) == 0 {
		return nil
	}

	widths := make(map[string]float64, g.NodeCount())

	// Iterate actual row IDs rather than assuming consecutive rows starting
	// at 0; gapped or shifted rows would otherwise silently get zero widths.
	if topRow := orders[rows[0]]; len(topRow) > 0 {
		unit := frameWidth / float64(len(topRow))
		for _, id := range topRow {
			widths[id] = unit
		}
	}

	for i := 0; i+1 < len(rows); i++ {
		r, next := rows[i], rows[i+1]
		currRow := orders[next]
		if len(currRow) == 0 {
			continue
		}

		for _, id := range currRow {
			widths[id] = 0.0
		}

		for _, parent := range orders[r] {
			kids := g.ChildrenInRow(parent, next)
			if n := len(kids); n > 0 {
				share := widths[parent] / float64(n)
				for _, kid := range kids {
					widths[kid] += share
				}
			}
		}

		var sum float64
		for _, id := range currRow {
			sum += widths[id]
		}

		if sum > eps && math.Abs(sum-frameWidth) > eps {
			scale := frameWidth / sum
			for _, id := range currRow {
				widths[id] *= scale
			}
		}
	}
	return widths
}

// ComputeWidthsBottomUp assigns horizontal widths to nodes by distributing
// the frame width among bottom-level nodes and propagating that width up
// to parents. This results in "bottom-heavy" towers where the leaf nodes
// provide a wide base.
func ComputeWidthsBottomUp(g *dag.DAG, orders map[int][]string, frameWidth float64) map[string]float64 {
	rows := g.RowIDs()
	if len(rows) == 0 {
		return nil
	}

	widths := make(map[string]float64, g.NodeCount())

	// Iterate actual row IDs rather than assuming consecutive rows starting
	// at 0 (see ComputeWidths).
	if bottomRow := orders[rows[len(rows)-1]]; len(bottomRow) > 0 {
		unit := frameWidth / float64(len(bottomRow))
		for _, id := range bottomRow {
			widths[id] = unit
		}
	}

	for i := len(rows) - 2; i >= 0; i-- {
		r, next := rows[i], rows[i+1]
		currRow := orders[r]
		if len(currRow) == 0 {
			continue
		}

		for _, id := range currRow {
			widths[id] = 0.0
		}

		for _, parent := range currRow {
			kids := g.ChildrenInRow(parent, next)
			if len(kids) == 0 {
				continue
			}
			for _, kid := range kids {
				parents := g.ParentsInRow(kid, r)
				if len(parents) > 0 {
					widths[parent] += widths[kid] / float64(len(parents))
				}
			}
		}

		var sum float64
		for _, id := range currRow {
			sum += widths[id]
		}

		if sum > eps && math.Abs(sum-frameWidth) > eps {
			scale := frameWidth / sum
			for _, id := range currRow {
				widths[id] *= scale
			}
		}
	}

	return widths
}
