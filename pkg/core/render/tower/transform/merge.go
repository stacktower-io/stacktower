package transform

import (
	"fmt"
	"slices"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/layout"
)

// MergeSubdividers combines subdivider blocks into continuous vertical columns.
// Subdivider nodes (created by [dag/transform.Subdivide] to break long edges)
// are grouped by their MasterID and horizontal position, then merged into
// single blocks spanning from top to bottom.
//
// This creates cleaner visuals where a package's vertical "column" is rendered
// as one continuous block rather than separate segments per row.
//
// The returned layout has subdivider nodes removed from RowOrders and replaced
// with merged blocks keyed by their master ID.
func MergeSubdividers(l layout.Layout, g *dag.DAG) layout.Layout {
	blocks := make(map[string]layout.Block)

	for master, members := range groupByMaster(g) {
		subgroups := groupByPosition(l, g, members)
		for _, group := range subgroups {
			b := merge(group.blocks, master)
			key := master
			// Only add @position suffix for subdivider-only groups when there are
			// multiple groups. The group containing the master keeps the master's ID
			// to match RowOrders.
			if len(subgroups) > 1 && !group.containsMaster {
				key = fmt.Sprintf("%s@%.0f-%.0f", master, b.Left, b.Right)
			}
			blocks[key] = b
		}
	}

	return layout.Layout{
		FrameWidth:  l.FrameWidth,
		FrameHeight: l.FrameHeight,
		Blocks:      blocks,
		RowOrders:   filterSubdividers(l.RowOrders, g),
		MarginX:     l.MarginX,
		MarginY:     l.MarginY,
	}
}

func groupByMaster(g *dag.DAG) map[string][]string {
	groups := make(map[string][]string)
	for _, n := range g.Nodes() {
		groups[n.EffectiveID()] = append(groups[n.EffectiveID()], n.ID)
	}
	return groups
}

type positionGroup struct {
	blocks         []layout.Block
	containsMaster bool
}

func groupByPosition(l layout.Layout, g *dag.DAG, members []string) []positionGroup {
	type pos struct{ l, r int }
	groups := make(map[pos]*positionGroup)

	for _, id := range members {
		if b, ok := l.Blocks[id]; ok {
			key := pos{int(b.Left + 0.5), int(b.Right + 0.5)}
			if groups[key] == nil {
				groups[key] = &positionGroup{}
			}
			groups[key].blocks = append(groups[key].blocks, b)
			// Check if this member is the master node itself (not a subdivider)
			if n, ok := g.Node(id); ok && !n.IsSubdivider() {
				groups[key].containsMaster = true
			}
		}
	}

	result := make([]positionGroup, 0, len(groups))
	for _, grp := range groups {
		result = append(result, *grp)
	}
	slices.SortFunc(result, func(a, b positionGroup) int {
		ab, bb := merge(a.blocks, ""), merge(b.blocks, "")
		if ab.Left != bb.Left {
			if ab.Left < bb.Left {
				return -1
			}
			return 1
		}
		if ab.Right != bb.Right {
			if ab.Right < bb.Right {
				return -1
			}
			return 1
		}
		if a.containsMaster != b.containsMaster {
			if a.containsMaster {
				return -1
			}
			return 1
		}
		return 0
	})
	return result
}

func merge(blocks []layout.Block, master string) layout.Block {
	if len(blocks) == 0 {
		return layout.Block{NodeID: master}
	}
	result := blocks[0]
	for _, b := range blocks[1:] {
		result.Bottom = min(result.Bottom, b.Bottom)
		result.Top = max(result.Top, b.Top)
	}
	result.NodeID = master
	return result
}

func filterSubdividers(orders map[int][]string, g *dag.DAG) map[int][]string {
	result := make(map[int][]string, len(orders))
	for row, ids := range orders {
		var filtered []string
		for _, id := range ids {
			if n, ok := g.Node(id); ok && !n.IsSubdivider() {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) > 0 {
			result[row] = filtered
		}
	}
	return result
}
