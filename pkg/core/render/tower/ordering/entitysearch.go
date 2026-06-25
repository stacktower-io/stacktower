package ordering

import (
	"cmp"
	"context"
	"slices"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// maxEntitySearchEntities bounds the size of graphs the entity search
// attempts. Larger graphs fall back to the row search + sifting alone.
const maxEntitySearchEntities = 60

// maxEntitySearchTrials caps the number of insertion trials so degenerate
// instances cannot consume the whole ordering budget.
const maxEntitySearchTrials = 2_000_000

// entEdge is a graph edge mapped onto entity endpoints: a is the entity of
// the upper-row node, b the entity of the lower-row node (possibly equal,
// for an edge internal to a column).
type entEdge struct {
	a, b int
}

// searchEntities runs an exact, anytime branch-and-bound over a single
// global left-to-right order of entities (rigid columns and single blocks).
//
// Key property: a layout in which every column occupies one consistent
// position across all of its rows is described exactly by a global order on
// entities — each row's order is its restriction to the entities present in
// that row. Searching this space directly makes coordinated multi-row moves
// (the ones the per-row candidate enumeration structurally misses) ordinary
// single moves, and only explores layouts the rigid-column renderer can
// actually honor.
//
// Entities are inserted one at a time; crossings among placed entities never
// decrease as more are inserted, so any partial order whose cost reaches the
// best known bound is pruned. The search is seeded with the current best
// score, returns improved row orders when found, and reports whether the
// space was exhausted (a true optimality proof for column-consistent
// layouts) or the search was cut short.
func searchEntities(ctx context.Context, g *dag.DAG, seed map[int][]string, budget int) (map[int][]string, int, bool) {
	rows := g.RowIDs()
	if len(rows) < 2 {
		return nil, 0, false
	}
	rowIdx := make(map[int]int, len(rows))
	for i, r := range rows {
		rowIdx[r] = i
	}

	ents := buildEntities(g)
	n := len(ents)
	if n < 2 || n > maxEntitySearchEntities {
		return nil, 0, false
	}

	entOf := make(map[string]int, g.NodeCount())
	for i, e := range ents {
		for _, id := range e.ids {
			entOf[id] = i
		}
	}

	edgesByPair := make([][]entEdge, len(rows)-1)
	for _, e := range g.EdgesIter() {
		u, uOK := g.Node(e.From)
		v, vOK := g.Node(e.To)
		if !uOK || !vOK || v.Row != u.Row+1 {
			// Multi-row edge: the graph is not normalized; bail out.
			return nil, 0, false
		}
		p := rowIdx[u.Row]
		edgesByPair[p] = append(edgesByPair[p], entEdge{entOf[e.From], entOf[e.To]})
	}

	// Seed positions (mean fractional position of the entity's members in
	// the seed ordering) steer gap exploration toward the known-good layout
	// so improvements are found early.
	seedPos := make([]float64, n)
	for i, e := range ents {
		sum, cnt := 0.0, 0
		for k, r := range e.rows {
			row := seed[r]
			if idx := slices.Index(row, e.ids[k]); idx >= 0 && len(row) > 1 {
				sum += float64(idx) / float64(len(row)-1)
				cnt++
			}
		}
		if cnt > 0 {
			seedPos[i] = sum / float64(cnt)
		}
	}

	// Most-constrained-first insertion order maximizes early pruning.
	deg := make([]int, n)
	for _, pe := range edgesByPair {
		for _, e := range pe {
			deg[e.a]++
			deg[e.b]++
		}
	}
	insOrder := make([]int, n)
	for i := range insOrder {
		insOrder[i] = i
	}
	slices.SortFunc(insOrder, func(a, b int) int {
		if c := cmp.Compare(deg[b], deg[a]); c != 0 {
			return c
		}
		return cmp.Compare(ents[a].ids[0], ents[b].ids[0])
	})

	s := &entSolver{
		ctx:         ctx,
		edgesByPair: edgesByPair,
		insOrder:    insOrder,
		seedPos:     seedPos,
		pos:         make([]int, n),
		best:        budget,
	}
	for i := range s.pos {
		s.pos[i] = -1
	}
	s.dfs(0)

	// Finding a zero-crossing layout terminates the search early but is
	// still a proof: nothing can beat zero.
	exhausted := s.exhausted || s.best == 0

	if s.bestSeq == nil {
		return nil, budget, exhausted
	}

	// Induce per-row orders from the winning global entity order.
	result := make(map[int][]string, len(rows))
	for _, entIdx := range s.bestSeq {
		e := ents[entIdx]
		for k, r := range e.rows {
			result[r] = append(result[r], e.ids[k])
		}
	}
	return result, s.best, exhausted
}

type entSolver struct {
	ctx         context.Context
	edgesByPair [][]entEdge
	insOrder    []int
	seedPos     []float64

	seq     []int    // current partial global order (entity indices)
	pos     []int    // entity -> position in seq, -1 if unplaced
	scratch [][2]int // reused by cost() to avoid hot-loop allocations

	best      int
	bestSeq   []int
	trials    int
	aborted   bool
	exhausted bool
}

func (s *entSolver) dfs(depth int) {
	if s.aborted || s.best == 0 {
		return
	}
	if depth == len(s.insOrder) {
		// partialCost was checked < best before descending.
		s.best = s.cost()
		s.bestSeq = slices.Clone(s.seq)
		return
	}

	x := s.insOrder[depth]

	// Preferred gap: number of placed entities to the seed-left of x.
	preferred := 0
	for _, e := range s.seq {
		if s.seedPos[e] < s.seedPos[x] {
			preferred++
		}
	}
	gaps := make([]int, len(s.seq)+1)
	for i := range gaps {
		gaps[i] = i
	}
	slices.SortFunc(gaps, func(a, b int) int {
		da, db := absInt(a-preferred), absInt(b-preferred)
		if c := cmp.Compare(da, db); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})

	for _, gap := range gaps {
		if s.trials >= maxEntitySearchTrials || s.ctx.Err() != nil {
			s.aborted = true
			return
		}
		s.trials++

		s.insert(x, gap)
		if s.cost() < s.best {
			s.dfs(depth + 1)
		}
		s.remove(x, gap)

		if s.aborted || s.best == 0 {
			return
		}
	}
	if depth == 0 {
		s.exhausted = !s.aborted
	}
}

func (s *entSolver) insert(x, gap int) {
	s.seq = slices.Insert(s.seq, gap, x)
	for i := gap; i < len(s.seq); i++ {
		s.pos[s.seq[i]] = i
	}
}

func (s *entSolver) remove(x, gap int) {
	s.seq = slices.Delete(s.seq, gap, gap+1)
	for i := gap; i < len(s.seq); i++ {
		s.pos[s.seq[i]] = i
	}
	s.pos[x] = -1
}

// cost counts crossings among the currently placed entities. Edges with an
// unplaced endpoint are ignored; as entities are inserted the cost is
// monotonically non-decreasing, which makes branch-and-bound pruning sound.
func (s *entSolver) cost() int {
	total := 0
	for _, pe := range s.edgesByPair {
		placed := s.scratch[:0]
		for _, e := range pe {
			pa, pb := s.pos[e.a], s.pos[e.b]
			if pa >= 0 && pb >= 0 {
				placed = append(placed, [2]int{pa, pb})
			}
		}
		for i := 0; i < len(placed); i++ {
			for j := i + 1; j < len(placed); j++ {
				if (placed[i][0]-placed[j][0])*(placed[i][1]-placed[j][1]) < 0 {
					total++
				}
			}
		}
		s.scratch = placed
	}
	return total
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
