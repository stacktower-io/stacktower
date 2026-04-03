package ordering

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/dag/perm"
)

const maxCandidatesBase = 10000

// maxRowWidth is the maximum number of nodes in a single row before falling
// back to barycentric ordering. Rows wider than this have factorial search
// spaces too large for optimal search, even with PQ-tree pruning.
const maxRowWidth = 30

// OptimalSearch implements a branch-and-bound search algorithm to find the
// mathematically optimal horizontal ordering (minimum crossings). It uses
// PQ-trees to prune the search space to only include orderings that satisfy
// structural constraints (Consecutive Ones Property).
type OptimalSearch struct {
	Progress func(explored, pruned, best int)
	Timeout  time.Duration
	Debug    func(info DebugInfo)
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

	// Check for rows too wide for optimal search - fall back to barycentric
	// to avoid factorial memory explosion
	for _, r := range rows {
		if len(g.NodesInRow(r)) > maxRowWidth {
			return Barycentric{}.OrderRows(g)
		}
	}

	timeout := o.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	initial := Barycentric{}.OrderRows(g)
	initialScore := dag.CountCrossings(g, initial)
	if initialScore == 0 {
		o.report(1, 0, 0)
		return initial
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	s.bestScore.Store(int64(initialScore))
	s.bestPath.Store(toIndexPath(g, rows, initial))

	for _, r := range rows {
		s.rowNodes[r] = g.NodesInRow(r)
	}

	if o.Progress != nil {
		go s.monitor(o.Progress)
	}

	s.search()

	if o.Progress != nil {
		o.report(int(s.explored.Load()), int(s.pruned.Load()), int(s.bestScore.Load()))
	}

	if o.Debug != nil {
		o.Debug(s.collectDebugInfo(initial))
	}

	return toStringOrder(s.rowNodes, s.rows, s.bestPath.Load().([][]int))
}

func (o OptimalSearch) report(explored, pruned, best int) {
	if o.Progress != nil {
		o.Progress(explored, pruned, best)
	}
}

type solver struct {
	g         *dag.DAG
	fg        *fastGraph
	rows      []int
	rowNodes  map[int][]*dag.Node
	candLimit int

	bestScore atomic.Int64
	bestPath  atomic.Value
	explored  atomic.Int64
	pruned    atomic.Int64
	maxDepth  atomic.Int64

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
		if s.bestScore.Load() == 0 {
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

			if score >= int(s.bestScore.Load()) {
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
	parallelNodes := s.rowNodes[s.rows[parallelRow]]
	n := len(parallelNodes)

	var starts [][]int
	if parallelRow == 0 {
		if n <= 8 {
			starts = perm.Generate(n, -1)
		} else {
			starts = perm.Generate(n, workerLimit)
		}
	} else {
		prevNodes := s.rowNodes[s.rows[parallelRow-1]]
		starts = s.generateC1PCandidates(parallelRow, parallelNodes, prefix[parallelRow-1], prevNodes)
		if len(starts) > workerLimit {
			starts = starts[:workerLimit]
		}

		prevPos := make(map[string]int, len(prefix[parallelRow-1]))
		for i, idx := range prefix[parallelRow-1] {
			prevPos[prevNodes[idx].ID] = i
		}
		sortByBarycenter(starts, s.g, parallelNodes, prevPos)
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

	if score >= int(s.bestScore.Load()) {
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
	prevNodes := s.rowNodes[s.rows[depth-1]]
	prevPos := make(map[string]int, len(prevOrder))
	for i, idx := range prevOrder {
		prevPos[prevNodes[idx].ID] = i
	}

	candidates := s.generateC1PCandidates(depth, nodes, prevOrder, prevNodes)
	sortByBarycenter(candidates, s.g, nodes, prevPos)

	for _, candidate := range candidates {
		newScore := score + dag.CountCrossingsIdx(s.fg.edges[depth-1], prevOrder, candidate, ws)
		if newScore >= int(s.bestScore.Load()) {
			s.pruned.Add(1)
			continue
		}

		path[depth] = candidate
		s.dfs(depth+1, newScore, path, ws)

		if s.bestScore.Load() == 0 || s.ctx.Err() != nil {
			return
		}
	}
}

func (s *solver) generateC1PCandidates(depth int, nodes []*dag.Node, prevOrder []int, prevNodes []*dag.Node) [][]int {
	n := len(nodes)
	if n <= 1 {
		return [][]int{perm.Seq(n)}
	}

	// For wide rows, limit candidates more aggressively to prevent memory issues
	// Each candidate is n ints (8 bytes each), so 1000 candidates × 20 nodes = 160KB
	limit := s.candLimit
	if n > 15 {
		// Scale down limit for wider rows: 20 nodes → 500, 25 nodes → 400, 30 nodes → 333
		limit = min(limit, s.candLimit*15/n)
	}

	nodeIdx := buildNodeIndex(nodes)
	tree := perm.NewPQTree(n)

	if !s.applyParentConstraints(tree, nodeIdx, depth, prevOrder, prevNodes) {
		return s.fallbackPermutations(n)
	}
	if !s.applyChildConstraints(tree, nodeIdx, depth) {
		return s.fallbackPermutations(n)
	}

	if n <= 8 {
		// For small rows, use actual count but cap at candidate limit
		// to prevent combinatorial explosion (e.g., 6! = 720 candidates
		// combined with other large rows creates 720M+ search paths)
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

func (s *solver) applyParentConstraints(tree *perm.PQTree, nodeIdx map[string]int, depth int, prevOrder []int, prevNodes []*dag.Node) bool {
	row := s.rows[depth]
	for _, idx := range prevOrder {
		children := s.g.ChildrenInRow(prevNodes[idx].ID, row)
		if constraint := idsToIndices(children, nodeIdx); len(constraint) >= 2 {
			if !tree.Reduce(constraint) {
				return false
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
			if !tree.Reduce(constraint) {
				return false
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

	for {
		current := int(s.bestScore.Load())
		if score >= current {
			return
		}
		if s.bestScore.CompareAndSwap(int64(current), int64(score)) {
			cloned := make([][]int, len(path))
			for i, p := range path {
				cloned[i] = slices.Clone(p)
			}
			s.bestPath.Store(cloned)
			if score == 0 {
				s.cancel()
			}
			return
		}
	}
}

func (s *solver) collectDebugInfo(initialOrder map[int][]string) DebugInfo {
	info := DebugInfo{
		TotalRows: len(s.rows),
		MaxDepth:  int(s.maxDepth.Load()),
		Rows:      make([]RowDebugInfo, len(s.rows)),
	}

	path := toIndexPath(s.g, s.rows, initialOrder)

	for i, r := range s.rows {
		nodes := s.rowNodes[r]
		rowInfo := RowDebugInfo{
			Row:       r,
			NodeCount: len(nodes),
		}

		if len(nodes) <= 1 {
			rowInfo.Candidates = 1
		} else if i == 0 {
			rowInfo.Candidates = min(perm.Factorial(len(nodes)), s.candLimit)
		} else {
			prevNodes := s.rowNodes[s.rows[i-1]]
			candidates := s.generateC1PCandidates(i, nodes, path[i-1], prevNodes)
			rowInfo.Candidates = len(candidates)
		}

		info.Rows[i] = rowInfo
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
			fn(int(s.explored.Load()), int(s.pruned.Load()), int(s.bestScore.Load()))
		}
	}
}

type fastGraph struct {
	edges       [][][]int
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
		for j, node := range upper {
			children := g.ChildrenInRow(node.ID, rows[i+1])
			targets := make([]int, 0, len(children))
			for _, child := range children {
				if idx, ok := lowerIdx[child]; ok {
					targets = append(targets, idx)
				}
			}
			slices.Sort(targets)
			fg.edges[i][j] = targets
		}
	}
	return fg
}

func sortByBarycenter(perms [][]int, g *dag.DAG, nodes []*dag.Node, prevPos map[string]int) {
	type scored struct {
		perm  []int
		score float64
	}
	s := make([]scored, len(perms))
	for i, p := range perms {
		s[i] = scored{p, barycenterDeviationIndices(g, nodes, p, prevPos, true)}
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
