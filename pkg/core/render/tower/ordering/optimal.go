package ordering

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/dag/perm"
)

const maxCandidatesBase = 10000

// maxRowWidth is the maximum number of nodes in a single row before falling
// back to barycentric ordering. Rows wider than this have factorial search
// spaces too large for optimal search, even with PQ-tree pruning.
const maxRowWidth = 30

// OptimalSearch implements a constrained anytime branch-and-bound search to
// minimize edge crossings. Candidate permutations per row are capped, PQ-tree
// constraints restrict the search space, and OSCM lower bounds prune branches
// early. The result is the best ordering found within the time budget — not a
// proven global optimum.
type OptimalSearch struct {
	Progress func(explored, pruned, best int)
	Timeout  time.Duration
	Debug    func(info DebugInfo)

	// Outcome, if set, receives a summary of how the search ended so
	// callers can label results honestly (proven optimal vs. best effort).
	Outcome func(info OutcomeInfo)

	// Ctx optionally provides a parent context so callers can cancel the
	// search (e.g. on SIGINT) before the Timeout elapses. When cancelled,
	// OrderRows returns the best ordering found so far. A nil Ctx behaves
	// like context.Background().
	Ctx context.Context
}

// OutcomeInfo describes how an optimal search concluded. Note that even a
// search that runs to completion is not a proof of optimality (candidate
// enumeration per row is capped), so callers should label results as
// "optimized" rather than "optimal".
type OutcomeInfo struct {
	// Crossings is the crossing count of the returned ordering.
	Crossings int
	// TimedOut is true when the timeout or caller cancellation stopped the
	// search before completion; the result is the best found so far.
	TimedOut bool
	// Fallback is true when a row exceeded the searchable width and the
	// barycentric heuristic was used instead of branch-and-bound search.
	Fallback bool
}

// DebugInfo contains diagnostic information about the optimal search process.
type DebugInfo struct {
	Rows      []RowDebugInfo
	MaxDepth  int
	TotalRows int
}

// RowDebugInfo contains diagnostic information for a single row during search.
type RowDebugInfo struct {
	Row        int
	NodeCount  int
	Candidates int
}

// OrderRows implements the [Orderer] interface by performing an optimal search.
func (o OptimalSearch) OrderRows(g *dag.DAG) map[int][]string {
	rows := g.RowIDs()
	if len(rows) == 0 {
		return nil
	}

	timeout := o.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	initial := Barycentric{}.OrderRows(g)
	initialScore := dag.CountCrossings(g, initial)
	if initialScore == 0 {
		o.report(1, 0, 0)
		o.reportOutcome(OutcomeInfo{Crossings: 0})
		return initial
	}

	parent := o.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	s := &solver{
		g:         g,
		fg:        newFastGraph(g, rows),
		rows:      rows,
		rowNodes:  make(map[int][]*dag.Node, len(rows)),
		candLimit: calcCandidateLimit(len(rows)),
		ctx:       ctx,
		cancel:    cancel,
	}
	s.best.Store(&bestResult{
		score: initialScore,
		path:  toIndexPath(g, rows, initial),
	})

	for _, r := range rows {
		s.rowNodes[r] = g.NodesInRow(r)
	}
	s.precomputeCandidates()
	s.computeLowerBounds()

	if o.Progress != nil {
		go s.monitor(o.Progress)
	}

	s.search()

	best := s.best.Load()

	if o.Progress != nil {
		o.report(int(s.explored.Load()), int(s.pruned.Load()), best.score)
	}

	if o.Debug != nil {
		o.Debug(s.collectDebugInfo(initial))
	}

	// A zero-crossing result cancels the context itself, so check the score
	// before attributing the early exit to the timeout.
	timedOut := best.score != 0 && ctx.Err() != nil

	result := toStringOrder(s.rowNodes, s.rows, best.path)
	score := best.score

	// Entity-aware refinement: the search enumerates candidates per row
	// independently, so coordinated multi-row moves (sliding a whole
	// pillar or pipe column sideways) and single-row moves lost to the
	// candidate cap can be missed. Sifting entities recovers both.
	if score > 0 {
		if refined := RefinePlacement(parent, g, result); refined < score {
			score = refined
		}
	}

	// Entity branch-and-bound: for small graphs, exactly search the space
	// of column-consistent layouts (a global order on rigid columns and
	// single blocks). This covers every coordinated move at once; the
	// sifted score above seeds the bound so pruning is tight.
	if score > 0 {
		if entOrders, entScore, _ := searchEntities(ctx, g, result, score); entOrders != nil && entScore < score {
			result, score = entOrders, entScore
		}
	}

	o.reportOutcome(OutcomeInfo{
		Crossings: score,
		TimedOut:  timedOut,
	})

	return result
}

func (o OptimalSearch) report(explored, pruned, best int) {
	if o.Progress != nil {
		o.Progress(explored, pruned, best)
	}
}

func (o OptimalSearch) reportOutcome(info OutcomeInfo) {
	if o.Outcome != nil {
		o.Outcome(info)
	}
}

// bestResult bundles the best score and path so they can be stored and
// swapped atomically, avoiding the race where bestScore and bestPath
// are updated in two separate atomic operations.
type bestResult struct {
	score int
	path  [][]int
}

type solver struct {
	g         *dag.DAG
	fg        *fastGraph
	rows      []int
	rowNodes  map[int][]*dag.Node
	candLimit int

	// cachedCandidates[depth] holds the precomputed C1P-valid permutations
	// for each row. The constraint set depends only on graph structure, not
	// on the chosen previous-row permutation, so we build once and reuse.
	cachedCandidates [][][]int

	// lowerBound[depth] is the minimum unavoidable crossings from layer
	// pair depth..len(rows)-1. Used to prune DFS branches early.
	lowerBound []int

	best     atomic.Pointer[bestResult]
	explored atomic.Int64
	pruned   atomic.Int64
	maxDepth atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
}

func calcCandidateLimit(numRows int) int {
	if numRows <= 3 {
		return maxCandidatesBase
	}
	// Linear scaling: more rows = fewer candidates per row
	// 5 rows → 2000, 10 rows → 1000, 20 rows → 500
	// Cap at 2000 max to prevent memory issues with wide rows
	limit := maxCandidatesBase / numRows
	return max(100, min(2000, limit))
}

func (s *solver) search() {
	workers := runtime.GOMAXPROCS(0)
	parallelRow := s.findParallelRow()

	prefix, prefixScore := s.buildPrefix(parallelRow)
	starts := s.generateStartPermutations(parallelRow, prefix, workers*100)

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

dispatch:
	for _, startPerm := range starts {
		if s.best.Load().score == 0 {
			break
		}

		// Acquire worker slot, respecting context timeout
		select {
		case sem <- struct{}{}:
		case <-s.ctx.Done():
			break dispatch
		}

		wg.Add(1)
		go func(start []int) {
			defer wg.Done()
			defer func() { <-sem }()

			if s.ctx.Err() != nil {
				return
			}

			path := make([][]int, len(s.rows))
			copy(path, prefix)
			path[parallelRow] = start

			score := prefixScore
			if parallelRow > 0 {
				ws := dag.NewCrossingWorkspace(s.fg.maxRowWidth)
				score += dag.CountCrossingsIdx(s.fg.edges[parallelRow-1], prefix[parallelRow-1], start, ws)
			}

			if score+s.lowerBound[parallelRow+1] >= s.best.Load().score {
				s.pruned.Add(1)
				return
			}

			s.dfs(parallelRow+1, score, path, dag.NewCrossingWorkspace(s.fg.maxRowWidth))
		}(startPerm)
	}

	wg.Wait()
}

func (s *solver) findParallelRow() int {
	for i, r := range s.rows {
		if len(s.rowNodes[r]) > 1 {
			return i
		}
	}
	return 0
}

func (s *solver) buildPrefix(parallelRow int) ([][]int, int) {
	prefix := make([][]int, len(s.rows))
	score := 0
	ws := dag.NewCrossingWorkspace(s.fg.maxRowWidth)

	for depth := 0; depth < parallelRow; depth++ {
		order := perm.Seq(len(s.rowNodes[s.rows[depth]]))
		prefix[depth] = order
		if depth > 0 {
			score += dag.CountCrossingsIdx(s.fg.edges[depth-1], prefix[depth-1], order, ws)
		}
	}
	return prefix, score
}

func (s *solver) generateStartPermutations(parallelRow int, prefix [][]int, workerLimit int) [][]int {
	starts := slices.Clone(s.cachedCandidates[parallelRow])

	if parallelRow > 0 {
		sortByBarycenterIdx(starts, s.fg.parents[parallelRow-1], prefix[parallelRow-1])
	}

	if len(starts) > workerLimit {
		starts = starts[:workerLimit]
	}
	return starts
}

func (s *solver) dfs(depth, score int, path [][]int, ws *dag.CrossingWorkspace) {
	if s.ctx.Err() != nil {
		return
	}

	// Track max depth reached
	for {
		cur := s.maxDepth.Load()
		if int64(depth) <= cur || s.maxDepth.CompareAndSwap(cur, int64(depth)) {
			break
		}
	}

	if score+s.lowerBound[depth] >= s.best.Load().score {
		s.pruned.Add(1)
		return
	}

	if depth == len(s.rows) {
		s.updateBest(path, score)
		return
	}

	rowID := s.rows[depth]
	nodes := s.rowNodes[rowID]
	if len(nodes) == 0 {
		path[depth] = nil
		s.dfs(depth+1, score, path, ws)
		return
	}

	prevOrder := path[depth-1]

	candidates := slices.Clone(s.cachedCandidates[depth])
	sortByBarycenterIdx(candidates, s.fg.parents[depth-1], prevOrder)

	for _, candidate := range candidates {
		newScore := score + dag.CountCrossingsIdx(s.fg.edges[depth-1], prevOrder, candidate, ws)
		if newScore >= s.best.Load().score {
			s.pruned.Add(1)
			continue
		}

		path[depth] = candidate
		s.dfs(depth+1, newScore, path, ws)

		if s.best.Load().score == 0 || s.ctx.Err() != nil {
			return
		}
	}
}

// computeLowerBounds fills s.lowerBound with suffix sums of the minimum
// unavoidable crossings per layer pair. For each pair of nodes u, v in the
// upper row sharing edges to the lower row, min(c_uv, c_vu) crossings are
// unavoidable regardless of ordering. The suffix sum lets the DFS prune
// branches where score + lowerBound[depth] >= best.
func (s *solver) computeLowerBounds() {
	nLayers := len(s.rows)
	s.lowerBound = make([]int, nLayers+1)

	for i := nLayers - 2; i >= 0; i-- {
		layerEdges := s.fg.edges[i]
		n := len(layerEdges)
		lb := 0
		for u := 0; u < n; u++ {
			for v := u + 1; v < n; v++ {
				cuv := countPairCrossingsIdx(layerEdges[u], layerEdges[v])
				cvu := countPairCrossingsIdx(layerEdges[v], layerEdges[u])
				lb += min(cuv, cvu)
			}
		}
		// lowerBound[i] covers layer pairs i..nLayers-2
		s.lowerBound[i] = lb + s.lowerBound[i+1]
	}
}

// countPairCrossingsIdx counts crossings between edges from node at position
// "left" and edges from node at position "right" (left < right in the upper
// row). A crossing occurs when a target of left is greater than a target of
// right. Both target slices must be sorted.
func countPairCrossingsIdx(leftTargets, rightTargets []int) int {
	crossings := 0
	j := 0
	for _, lt := range leftTargets {
		for j < len(rightTargets) && rightTargets[j] < lt {
			j++
		}
		crossings += j
	}
	return crossings
}

// precomputeCandidates builds the C1P-valid permutations for every depth once
// so the hot DFS path never touches PQ-trees or string-based graph lookups.
func (s *solver) precomputeCandidates() {
	s.cachedCandidates = make([][][]int, len(s.rows))
	for depth := range s.rows {
		s.cachedCandidates[depth] = s.buildC1PCandidates(depth)
	}
}

// buildC1PCandidates computes the set of structurally valid permutations for
// a single row. Constraints come from graph structure only (which parents
// share which children must be consecutive) and are order-independent.
func (s *solver) buildC1PCandidates(depth int) [][]int {
	nodes := s.rowNodes[s.rows[depth]]
	n := len(nodes)
	if n <= 1 {
		return [][]int{perm.Seq(n)}
	}

	// Rows wider than maxRowWidth have search spaces too large even with
	// PQ-tree pruning. Freeze them at a single candidate (the identity
	// permutation, which will be the barycentric-initialized order).
	if n > maxRowWidth {
		return [][]int{perm.Seq(n)}
	}

	limit := s.candLimit
	if n > 15 {
		limit = min(limit, s.candLimit*15/n)
	}

	nodeIdx := buildNodeIndex(nodes)
	tree := perm.NewPQTree(n)

	if !s.applyParentConstraints(tree, nodeIdx, depth) {
		return s.fallbackPermutations(n)
	}
	if !s.applyChildConstraints(tree, nodeIdx, depth) {
		return s.fallbackPermutations(n)
	}

	if n <= 8 {
		actualCount := tree.ValidCount()
		if actualCount <= limit {
			limit = actualCount
		}
	}

	perms := tree.Enumerate(limit)
	if len(perms) == 0 {
		return s.fallbackPermutations(n)
	}
	return perms
}

func (s *solver) applyParentConstraints(tree *perm.PQTree, nodeIdx map[string]int, depth int) bool {
	row := s.rows[depth]
	if depth == 0 {
		return true
	}
	for _, parent := range s.rowNodes[s.rows[depth-1]] {
		children := s.g.ChildrenInRow(parent.ID, row)
		if constraint := idsToIndices(children, nodeIdx); len(constraint) >= 2 {
			snapshot := tree.Clone()
			if !tree.Reduce(constraint) {
				*tree = *snapshot
			}
		}
	}
	return true
}

func (s *solver) applyChildConstraints(tree *perm.PQTree, nodeIdx map[string]int, depth int) bool {
	if depth >= len(s.rows)-1 {
		return true
	}
	row := s.rows[depth]
	for _, child := range s.rowNodes[s.rows[depth+1]] {
		parents := s.g.ParentsInRow(child.ID, row)
		if constraint := idsToIndices(parents, nodeIdx); len(constraint) >= 2 {
			snapshot := tree.Clone()
			if !tree.Reduce(constraint) {
				*tree = *snapshot
			}
		}
	}
	return true
}

func (s *solver) fallbackPermutations(n int) [][]int {
	if n <= 8 {
		return perm.Generate(n, -1)
	}
	return perm.Generate(n, s.candLimit)
}

func (s *solver) updateBest(path [][]int, score int) {
	s.explored.Add(1)

	cloned := make([][]int, len(path))
	for i, p := range path {
		cloned[i] = slices.Clone(p)
	}
	candidate := &bestResult{score: score, path: cloned}

	for {
		current := s.best.Load()
		if score >= current.score {
			return
		}
		if s.best.CompareAndSwap(current, candidate) {
			if score == 0 {
				s.cancel()
			}
			return
		}
	}
}

func (s *solver) collectDebugInfo(_ map[int][]string) DebugInfo {
	info := DebugInfo{
		TotalRows: len(s.rows),
		MaxDepth:  int(s.maxDepth.Load()),
		Rows:      make([]RowDebugInfo, len(s.rows)),
	}

	for i, r := range s.rows {
		info.Rows[i] = RowDebugInfo{
			Row:        r,
			NodeCount:  len(s.rowNodes[r]),
			Candidates: len(s.cachedCandidates[i]),
		}
	}

	return info
}

func (s *solver) monitor(fn func(int, int, int)) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			fn(int(s.explored.Load()), int(s.pruned.Load()), s.best.Load().score)
		}
	}
}

type fastGraph struct {
	edges       [][][]int
	parents     [][][]int // parents[i][j] = indices in row i of parents of row i+1 node j
	maxRowWidth int
}

func newFastGraph(g *dag.DAG, rows []int) *fastGraph {
	rowNodes := make(map[int][]*dag.Node, len(rows))
	maxWidth := 0
	for _, r := range rows {
		nodes := g.NodesInRow(r)
		rowNodes[r] = nodes
		if len(nodes) > maxWidth {
			maxWidth = len(nodes)
		}
	}

	fg := &fastGraph{
		edges:       make([][][]int, len(rows)-1),
		parents:     make([][][]int, len(rows)-1),
		maxRowWidth: maxWidth,
	}

	for i := 0; i < len(rows)-1; i++ {
		upper := rowNodes[rows[i]]
		lower := rowNodes[rows[i+1]]

		lowerIdx := make(map[string]int, len(lower))
		for j, n := range lower {
			lowerIdx[n.ID] = j
		}

		fg.edges[i] = make([][]int, len(upper))
		fg.parents[i] = make([][]int, len(lower))

		for j, node := range upper {
			children := g.ChildrenInRow(node.ID, rows[i+1])
			targets := make([]int, 0, len(children))
			for _, child := range children {
				if idx, ok := lowerIdx[child]; ok {
					targets = append(targets, idx)
					fg.parents[i][idx] = append(fg.parents[i][idx], j)
				}
			}
			slices.Sort(targets)
			fg.edges[i][j] = targets
		}
	}
	return fg
}

// sortByBarycenterIdx ranks candidate permutations by how closely they match
// the barycentric ideal: each node's position should equal the mean position
// of its parents in the previous row. Works entirely on integer indices to
// avoid string hashing and DAG lookups in the DFS hot path.
func sortByBarycenterIdx(perms [][]int, parentLists [][]int, prevPerm []int) {
	if len(perms) <= 1 {
		return
	}
	// Build inverse: prevPos[origIdx] = position in current permutation
	prevPos := make([]int, len(prevPerm))
	for pos, origIdx := range prevPerm {
		if origIdx < len(prevPos) {
			prevPos[origIdx] = pos
		}
	}

	type scored struct {
		perm  []int
		score float64
	}
	s := make([]scored, len(perms))
	for i, p := range perms {
		dev := 0.0
		for pos, origIdx := range p {
			parents := parentLists[origIdx]
			if len(parents) == 0 {
				continue
			}
			sum := 0
			for _, pidx := range parents {
				sum += prevPos[pidx]
			}
			bc := float64(sum) / float64(len(parents))
			delta := float64(pos) - bc
			dev += delta * delta
		}
		s[i] = scored{p, dev}
	}
	slices.SortFunc(s, func(a, b scored) int {
		return cmp.Compare(a.score, b.score)
	})
	for i, x := range s {
		perms[i] = x.perm
	}
}

func toIndexPath(g *dag.DAG, rows []int, order map[int][]string) [][]int {
	path := make([][]int, len(rows))
	for i, r := range rows {
		nodes := g.NodesInRow(r)
		nodeIdx := make(map[string]int, len(nodes))
		for j, n := range nodes {
			nodeIdx[n.ID] = j
		}
		indices := make([]int, len(nodes))
		for j, id := range order[r] {
			indices[j] = nodeIdx[id]
		}
		path[i] = indices
	}
	return path
}

func toStringOrder(rowNodes map[int][]*dag.Node, rows []int, path [][]int) map[int][]string {
	result := make(map[int][]string, len(rows))
	for i, r := range rows {
		if i >= len(path) || path[i] == nil {
			continue
		}
		nodes := rowNodes[r]
		ids := make([]string, len(path[i]))
		for j, idx := range path[i] {
			ids[j] = nodes[idx].ID
		}
		result[r] = ids
	}
	return result
}

func buildNodeIndex(nodes []*dag.Node) map[string]int {
	idx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		idx[n.ID] = i
	}
	return idx
}

func idsToIndices(ids []string, idx map[string]int) []int {
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if i, ok := idx[id]; ok {
			result = append(result, i)
		}
	}
	return result
}
