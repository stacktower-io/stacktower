package ordering

import (
	"context"
	"slices"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// maxRefineSweeps bounds how many times the refinement re-scans all
// entities; each sweep only repeats if the previous one found an improvement.
const maxRefineSweeps = 4

// refineSiftBudget caps the approximate work (entities × row width) spent
// sifting single-row entities. Above the budget only multi-row columns are
// refined, since those are the moves the row search cannot represent at all.
const refineSiftBudget = 100_000

// entity is a rigid vertical unit of the layout: one member per row across
// consecutive rows. Multi-row entities are subdivider chains (long-edge
// pipes, sink-extension pillars), optionally topped by their leaf master;
// single-row entities are all remaining nodes.
type entity struct {
	rows []int    // consecutive row IDs, top to bottom
	ids  []string // member ID per row, parallel to rows
}

// RefinePlacement improves an ordering with a sifting local search: every
// entity — rigid vertical columns and single blocks — is slid across all
// candidate positions as one unit, keeping any move that lowers the crossing
// count, until a sweep finds no improvement. It mutates orders in place and
// returns the final total crossing count.
//
// The branch-and-bound search enumerates candidates per row independently
// and caps the enumeration, so two families of moves can be missed: moves
// that only pay off when applied to several rows at once (sliding a whole
// column), and single-row moves whose permutation didn't survive the
// candidate cap. Sifting explores both directly on the final ordering.
func RefinePlacement(ctx context.Context, g *dag.DAG, orders map[int][]string) int {
	rows := g.RowIDs()
	if len(rows) < 2 {
		return dag.CountCrossings(g, orders)
	}
	rowIdx := make(map[int]int, len(rows))
	maxRowLen := 0
	for i, r := range rows {
		rowIdx[r] = i
		maxRowLen = max(maxRowLen, len(orders[r]))
	}

	ents := buildEntities(g)
	if len(ents)*maxRowLen > refineSiftBudget {
		ents = slices.DeleteFunc(ents, func(e entity) bool { return len(e.ids) < 2 })
	}

	pairCount := func(i int) int {
		return dag.CountLayerCrossings(g, orders[rows[i]], orders[rows[i+1]])
	}

	improved := true
	for sweep := 0; sweep < maxRefineSweeps && improved; sweep++ {
		improved = false
		for _, ent := range ents {
			if ctx != nil && ctx.Err() != nil {
				return dag.CountCrossings(g, orders)
			}

			// Crossings can only change in the row pairs touching the
			// entity's span.
			pairLo := max(rowIdx[ent.rows[0]]-1, 0)
			pairHi := min(rowIdx[ent.rows[len(ent.rows)-1]], len(rows)-2)
			base := 0
			for i := pairLo; i <= pairHi; i++ {
				base += pairCount(i)
			}
			if base == 0 {
				continue
			}

			orig := make([]int, len(ent.rows))
			maxPos := 0
			for k, r := range ent.rows {
				orig[k] = slices.Index(orders[r], ent.ids[k])
				maxPos = max(maxPos, len(orders[r])-1)
			}

			place := func(pos func(k int) int) {
				for k, r := range ent.rows {
					ids := orders[r]
					cur := slices.Index(ids, ent.ids[k])
					ids = slices.Delete(ids, cur, cur+1)
					orders[r] = slices.Insert(ids, min(pos(k), len(ids)), ent.ids[k])
				}
			}

			bestPos, bestScore := -1, base
			for p := 0; p <= maxPos; p++ {
				place(func(int) int { return p })
				score := 0
				for i := pairLo; i <= pairHi; i++ {
					score += pairCount(i)
				}
				if score < bestScore {
					bestScore, bestPos = score, p
				}
				place(func(k int) int { return orig[k] })
			}

			if bestPos >= 0 {
				place(func(int) int { return bestPos })
				improved = true
			}
		}
	}
	return dag.CountCrossings(g, orders)
}

// buildEntities partitions the graph's nodes into rigid vertical entities.
//
// For each master, its subdivider chain across consecutive rows forms one
// multi-row entity, topped by the master itself when the master is a leaf
// whose only child is the chain head (a foundation pillar). Masters with
// parallel chains (duplicate rows) or gapped chains contribute their
// subdividers as single-row entities instead. Every node not absorbed into
// a chain becomes a single-row entity.
//
// The result is deterministic: multi-row entities sorted by master, then
// singletons sorted by node ID.
func buildEntities(g *dag.DAG) []entity {
	subs := make(map[string]map[int]string)
	dup := make(map[string]bool)
	for _, n := range g.Nodes() {
		if !n.IsSubdivider() || n.MasterID == "" {
			continue
		}
		m := subs[n.MasterID]
		if m == nil {
			m = make(map[int]string)
			subs[n.MasterID] = m
		}
		if _, exists := m[n.Row]; exists {
			dup[n.MasterID] = true
			continue
		}
		m[n.Row] = n.ID
	}

	masters := make([]string, 0, len(subs))
	for m := range subs {
		if !dup[m] {
			masters = append(masters, m)
		}
	}
	slices.Sort(masters) // deterministic order

	inChain := make(map[string]bool)
	var ents []entity
	for _, master := range masters {
		rowSubs := subs[master]
		rowList := make([]int, 0, len(rowSubs))
		for r := range rowSubs {
			rowList = append(rowList, r)
		}
		slices.Sort(rowList)

		consecutive := true
		for i := 1; i < len(rowList); i++ {
			if rowList[i] != rowList[i-1]+1 {
				consecutive = false
				break
			}
		}
		if !consecutive {
			continue
		}

		ent := entity{rows: rowList, ids: make([]string, 0, len(rowList)+1)}
		for _, r := range rowList {
			ent.ids = append(ent.ids, rowSubs[r])
		}

		// A leaf master moves with its pillar: it is visually the top
		// segment of the same column.
		if mn, ok := g.Node(master); ok && mn.Row == rowList[0]-1 {
			if ch := g.Children(master); len(ch) == 1 && ch[0] == rowSubs[rowList[0]] {
				ent.rows = append([]int{mn.Row}, ent.rows...)
				ent.ids = append([]string{master}, ent.ids...)
			}
		}

		for _, id := range ent.ids {
			inChain[id] = true
		}
		ents = append(ents, ent)
	}

	singles := make([]string, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		if !inChain[n.ID] {
			singles = append(singles, n.ID)
		}
	}
	slices.Sort(singles)
	for _, id := range singles {
		n, _ := g.Node(id)
		ents = append(ents, entity{rows: []int{n.Row}, ids: []string{id}})
	}
	return ents
}
