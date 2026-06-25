package perm

import (
	"slices"
	"strings"
)

// PQTree is a data structure that compactly represents a family of permutations
// satisfying "consecutive ones" constraints.
//
// A PQ-tree encodes the valid orderings of n elements where certain subsets
// must appear consecutively. The tree represents all permutations that satisfy
// the applied constraints, allowing efficient pruning of invalid orderings.
//
// The tree has two types of internal nodes:
//   - P-nodes (Permutable): children can appear in any order (n! orderings)
//   - Q-nodes (seQuence): children have a fixed order, reversible (2 orderings)
//
// PQTree is not safe for concurrent use. If multiple goroutines access a PQTree,
// they must be synchronized with external locking.
//
// The zero value of PQTree is not usable; use NewPQTree to create instances.
type PQTree struct {
	root   *pqNode
	leaves []*pqNode
}

type nodeKind int

const (
	leafNode nodeKind = iota
	pNode
	qNode
)

type markKind int

const (
	unmarked markKind = iota
	empty
	full
	partial
)

type pqNode struct {
	kind         nodeKind
	value        int
	children     []*pqNode
	parent       *pqNode
	mark         markKind
	fullCount    int
	partialCount int
}

// NewPQTree creates a PQ-tree representing all n! permutations of n elements.
//
// The elements are numbered [0, 1, ..., n-1]. Initially, no constraints are
// applied, so all n! orderings are valid. Call Reduce to apply consecutive-ones
// constraints that restrict the set of valid permutations.
//
// For n = 0, NewPQTree returns a tree representing one empty permutation.
// For n = 1, NewPQTree returns a tree with a single element.
//
// The returned PQTree is ready to use and can be modified with Reduce or
// queried with Enumerate, ValidCount, and String methods.
//
// To explore multiple constraint branches without mutating the original tree,
// use Clone to create independent copies.
func NewPQTree(n int) *PQTree {
	if n == 0 {
		return &PQTree{}
	}
	if n == 1 {
		leaf := &pqNode{kind: leafNode, value: 0}
		return &PQTree{root: leaf, leaves: []*pqNode{leaf}}
	}

	leaves := make([]*pqNode, n)
	for i := range leaves {
		leaves[i] = &pqNode{kind: leafNode, value: i}
	}

	root := &pqNode{kind: pNode, children: slices.Clone(leaves)}
	for _, child := range leaves {
		child.parent = root
	}

	return &PQTree{root: root, leaves: leaves}
}

// Reduce applies a consecutive-ones constraint to the tree.
//
// After calling Reduce(constraint), only permutations where all elements in
// constraint appear consecutively (in any order) remain valid. Multiple calls
// to Reduce apply cumulative constraints, further restricting the valid set.
//
// Reduce returns true if the constraint is satisfiable with previously applied
// constraints, false if the constraint creates a contradiction. When Reduce
// returns false, the tree is left in an undefined state and should not be used
// further.
//
// The constraint slice is not modified. Element indices must be in the range
// [0, n-1] where n is the value passed to NewPQTree. Out-of-range indices are
// silently ignored.
//
// Trivial constraints (length 0, 1, or equal to tree size) are always satisfiable
// and have no effect on the tree structure.
//
// Example:
//
//	tree := perm.NewPQTree(5)
//	tree.Reduce([]int{1, 2, 3})  // Elements 1, 2, 3 must be consecutive
//	tree.Reduce([]int{0, 1})     // Elements 0, 1 must be consecutive
func (t *PQTree) Reduce(constraint []int) bool {
	if t.root == nil || len(constraint) <= 1 || len(constraint) == len(t.leaves) {
		return true
	}

	t.clearMarks(t.root)
	for _, elem := range constraint {
		if elem >= 0 && elem < len(t.leaves) {
			t.leaves[elem].mark = full
		}
	}

	if t.bubbleUp(t.root) == empty {
		return true
	}

	return t.reduce(t.root)
}

// Clone creates an independent deep copy of the PQ-tree.
//
// The cloned tree has identical structure and represents the same set of valid
// permutations, but can be modified independently without affecting the original.
// This is useful for exploring multiple constraint branches in search algorithms.
//
// Clone copies the entire internal tree structure. For large trees, this operation
// may be expensive. Consider cloning only when necessary.
//
// Example:
//
//	tree := perm.NewPQTree(5)
//	tree.Reduce([]int{1, 2, 3})
//
//	// Try two different additional constraints
//	branch1 := tree.Clone()
//	branch1.Reduce([]int{0, 1})  // Branch 1 constraint
//
//	branch2 := tree.Clone()
//	branch2.Reduce([]int{3, 4})  // Branch 2 constraint
//
//	// Original tree unchanged
//	fmt.Println(tree.ValidCount())
func (t *PQTree) Clone() *PQTree {
	if t.root == nil {
		return &PQTree{}
	}

	// Map old nodes to new nodes for parent/leaf pointer fixup
	nodeMap := make(map[*pqNode]*pqNode)

	// Clone the tree structure
	newRoot := t.cloneNode(t.root, nodeMap)

	// Rebuild leaves slice
	newLeaves := make([]*pqNode, len(t.leaves))
	for i, oldLeaf := range t.leaves {
		newLeaves[i] = nodeMap[oldLeaf]
	}

	return &PQTree{
		root:   newRoot,
		leaves: newLeaves,
	}
}

func (t *PQTree) cloneNode(n *pqNode, nodeMap map[*pqNode]*pqNode) *pqNode {
	if n == nil {
		return nil
	}

	// Check if already cloned (shouldn't happen in tree, but safe)
	if clone, exists := nodeMap[n]; exists {
		return clone
	}

	// Create new node with same basic fields
	clone := &pqNode{
		kind:  n.kind,
		value: n.value,
		mark:  n.mark,
	}
	nodeMap[n] = clone

	// Clone children recursively
	if len(n.children) > 0 {
		clone.children = make([]*pqNode, len(n.children))
		for i, child := range n.children {
			clone.children[i] = t.cloneNode(child, nodeMap)
			clone.children[i].parent = clone
		}
	}

	return clone
}

func (t *PQTree) clearMarks(n *pqNode) {
	n.mark = unmarked
	n.fullCount = 0
	n.partialCount = 0
	for _, c := range n.children {
		t.clearMarks(c)
	}
}

func (t *PQTree) bubbleUp(n *pqNode) markKind {
	if n.kind == leafNode {
		if n.mark == unmarked {
			n.mark = empty
		}
		return n.mark
	}

	n.fullCount = 0
	n.partialCount = 0
	for _, c := range n.children {
		switch t.bubbleUp(c) {
		case full:
			n.fullCount++
		case partial:
			n.partialCount++
		}
	}

	switch {
	case n.fullCount == len(n.children):
		n.mark = full
	case n.fullCount == 0 && n.partialCount == 0:
		n.mark = empty
	default:
		n.mark = partial
	}
	return n.mark
}

func (t *PQTree) reduce(n *pqNode) bool {
	if n.mark == full || n.mark == empty {
		return true
	}

	switch n.kind {
	case leafNode:
		return true
	case pNode:
		return t.reducePNode(n)
	case qNode:
		return t.reduceQNode(n)
	}
	return false
}

func (t *PQTree) reducePartialChildren(n *pqNode) bool {
	for _, c := range n.children {
		if c.mark == partial && !t.reduce(c) {
			return false
		}
	}
	n.fullCount = 0
	n.partialCount = 0
	for _, c := range n.children {
		switch c.mark {
		case full:
			n.fullCount++
		case partial:
			n.partialCount++
		}
	}
	return true
}

func (t *PQTree) reducePNode(n *pqNode) bool {
	if !t.reducePartialChildren(n) {
		return false
	}

	var fullCh, emptyCh, partialCh []*pqNode
	for _, child := range n.children {
		switch child.mark {
		case full:
			fullCh = append(fullCh, child)
		case empty:
			emptyCh = append(emptyCh, child)
		case partial:
			partialCh = append(partialCh, child)
		}
	}

	if len(partialCh) > 1 {
		return false
	}

	if len(fullCh) == 0 {
		return true
	}

	if len(partialCh) == 0 {
		if len(fullCh) > 1 && len(emptyCh) > 0 {
			t.groupChildren(n, fullCh, pNode)
		}
		return true
	}

	return t.extendPartialChild(n, partialCh[0], fullCh)
}

func (t *PQTree) reduceQNode(n *pqNode) bool {
	if !t.reducePartialChildren(n) {
		return false
	}

	first, last := -1, -1
	var partialIdx []int
	for i, child := range n.children {
		switch child.mark {
		case full:
			if first < 0 {
				first = i
			}
			last = i
		case partial:
			partialIdx = append(partialIdx, i)
		}
	}

	if first < 0 {
		return true
	}

	for i := first; i <= last; i++ {
		if n.children[i].mark == empty {
			return false
		}
	}

	for _, idx := range partialIdx {
		if idx != first-1 && idx != last+1 {
			return false
		}
	}

	for _, idx := range partialIdx {
		if n.children[idx].kind == qNode {
			t.mergeQNodes(n, idx)
		}
	}
	return true
}

func (t *PQTree) groupChildren(parent *pqNode, group []*pqNode, kind nodeKind) {
	if len(group) <= 1 {
		return
	}

	node := &pqNode{
		kind:     kind,
		children: slices.Clone(group),
		parent:   parent,
		mark:     group[0].mark,
	}

	for _, child := range group {
		child.parent = node
	}

	groupSet := make(map[*pqNode]bool, len(group))
	for _, child := range group {
		groupSet[child] = true
	}

	newChildren := make([]*pqNode, 0, len(parent.children)-len(group)+1)
	inserted := false
	for _, child := range parent.children {
		if groupSet[child] {
			if !inserted {
				newChildren = append(newChildren, node)
				inserted = true
			}
		} else {
			newChildren = append(newChildren, child)
		}
	}
	parent.children = newChildren
}

func (t *PQTree) extendPartialChild(parent, partialChild *pqNode, fullSiblings []*pqNode) bool {
	if len(fullSiblings) == 0 {
		return true
	}

	var fullInPartial, emptyInPartial []*pqNode
	for _, child := range partialChild.children {
		if child.mark == full {
			fullInPartial = append(fullInPartial, child)
		} else {
			emptyInPartial = append(emptyInPartial, child)
		}
	}

	children := make([]*pqNode, 0, len(fullInPartial)+len(fullSiblings)+len(emptyInPartial))
	children = append(children, emptyInPartial...)
	children = append(children, fullInPartial...)
	children = append(children, fullSiblings...)

	qnode := &pqNode{
		kind:     qNode,
		children: children,
		parent:   parent,
		mark:     partial,
	}
	for _, child := range children {
		child.parent = qnode
	}

	toRemove := make(map[*pqNode]bool, len(fullSiblings)+1)
	toRemove[partialChild] = true
	for _, sibling := range fullSiblings {
		toRemove[sibling] = true
	}

	newChildren := make([]*pqNode, 0, len(parent.children)-len(toRemove)+1)
	replaced := false
	for _, child := range parent.children {
		if toRemove[child] {
			if !replaced {
				newChildren = append(newChildren, qnode)
				replaced = true
			}
		} else {
			newChildren = append(newChildren, child)
		}
	}
	parent.children = newChildren

	if len(parent.children) == 1 && parent == t.root {
		t.root = parent.children[0]
		t.root.parent = nil
	}

	return true
}

func (t *PQTree) mergeQNodes(parent *pqNode, idx int) {
	child := parent.children[idx]
	if child.kind != qNode {
		return
	}

	reverse := false
	if idx > 0 && parent.children[idx-1].mark == full {
		if len(child.children) > 0 && child.children[len(child.children)-1].mark == full {
			reverse = true
		}
	} else if idx < len(parent.children)-1 && parent.children[idx+1].mark == full {
		if len(child.children) > 0 && child.children[0].mark == full {
			reverse = true
		}
	}

	if reverse {
		slices.Reverse(child.children)
	}

	for _, grandchild := range child.children {
		grandchild.parent = parent
	}

	newChildren := make([]*pqNode, 0, len(parent.children)+len(child.children)-1)
	newChildren = append(newChildren, parent.children[:idx]...)
	newChildren = append(newChildren, child.children...)
	newChildren = append(newChildren, parent.children[idx+1:]...)
	parent.children = newChildren
}

// Enumerate returns all valid permutations represented by the tree.
//
// If limit > 0, Enumerate returns at most limit permutations.
// If limit <= 0, Enumerate returns all valid permutations.
//
// Each returned slice is a separate allocation containing element indices in
// permuted order. The slices are safe to modify without affecting the tree or
// other returned permutations.
//
// The order of returned permutations is not specified and may change between
// calls or Go versions.
//
// For trees with a large ValidCount, always use a limit to avoid memory exhaustion.
// A tree with strong constraints might have only a few hundred valid orderings,
// while an unconstrained tree has n! orderings.
//
// For memory-efficient streaming without allocating all results at once, use
// EnumerateFunc instead.
//
// Example:
//
//	tree := perm.NewPQTree(4)
//	tree.Reduce([]int{0, 1, 2})
//	orderings := tree.Enumerate(10)  // Get first 10 valid orderings
func (t *PQTree) Enumerate(limit int) [][]int {
	if t.root == nil {
		return [][]int{{}}
	}

	var results [][]int
	buf := make([]int, 0, len(t.leaves))
	t.enumerateLazy(t.root, &buf, func(perm []int) bool {
		results = append(results, slices.Clone(perm))
		return limit <= 0 || len(results) < limit
	})
	return results
}

// EnumerateFunc generates valid permutations one at a time via callback.
//
// EnumerateFunc calls fn for each valid permutation until fn returns false or
// all permutations are exhausted. This is memory-efficient for large result sets
// since permutations are generated on-demand rather than allocated all at once.
//
// The callback fn receives a permutation slice that is valid only for the
// duration of the call. If the caller needs to retain the permutation, it must
// copy it (e.g., with slices.Clone).
//
// EnumerateFunc returns the number of permutations processed before stopping.
// If fn always returns true, the return value equals ValidCount().
//
// The order of generated permutations is not specified and may change between
// calls or Go versions.
//
// Example:
//
//	tree := perm.NewPQTree(10)
//	tree.Reduce([]int{0, 1, 2, 3, 4})
//
//	// Process first 100 permutations without allocating all at once
//	count := 0
//	tree.EnumerateFunc(func(perm []int) bool {
//		// Process perm here
//		fmt.Println(perm)
//		count++
//		return count < 100  // Stop after 100
//	})
func (t *PQTree) EnumerateFunc(fn func([]int) bool) int {
	if t.root == nil {
		fn([]int{})
		return 1
	}

	count := 0
	buf := make([]int, 0, len(t.leaves))
	t.enumerateLazy(t.root, &buf, func(perm []int) bool {
		count++
		return fn(perm)
	})
	return count
}

// enumerateLazy generates permutations one at a time via callback.
// Returns false if callback signaled stop, true otherwise.
// buf is a shared scratch buffer; callers must clone any permutation
// they want to keep (Enumerate does this; EnumerateFunc documents it).
func (t *PQTree) enumerateLazy(node *pqNode, buf *[]int, emit func([]int) bool) bool {
	if node.kind == leafNode {
		*buf = append(*buf, node.value)
		ok := emit(*buf)
		*buf = (*buf)[:len(*buf)-1]
		return ok
	}

	return t.forEachChildPerm(node, func(children []*pqNode) bool {
		return t.enumerateChildrenLazy(children, buf, emit)
	})
}

// For Q-nodes: yields forward and reverse only.
// For P-nodes: generates permutations one at a time without storing them all.
func (t *PQTree) forEachChildPerm(node *pqNode, fn func([]*pqNode) bool) bool {
	if node.kind == qNode {
		if !fn(node.children) {
			return false
		}
		if len(node.children) <= 1 {
			return true
		}
		rev := slices.Clone(node.children)
		slices.Reverse(rev)
		return fn(rev)
	}

	// P-node: Generate permutations lazily
	n := len(node.children)
	if n == 0 {
		return fn(nil)
	}
	if n == 1 {
		return fn(node.children)
	}

	perm := slices.Clone(node.children)
	state := make([]int, n)

	// Emit first permutation (identity)
	if !fn(slices.Clone(perm)) {
		return false
	}

	// iteratively generate remaining permutations
	for i := 0; i < n; {
		if state[i] < i {
			if i&1 == 0 {
				perm[0], perm[i] = perm[i], perm[0]
			} else {
				perm[state[i]], perm[i] = perm[i], perm[state[i]]
			}
			if !fn(slices.Clone(perm)) {
				return false
			}
			state[i]++
			i = 0
		} else {
			state[i] = 0
			i++
		}
	}
	return true
}

func (t *PQTree) enumerateChildrenLazy(children []*pqNode, buf *[]int, emit func([]int) bool) bool {
	if len(children) == 0 {
		return emit(*buf)
	}

	first := children[0]
	rest := children[1:]

	return t.enumerateLazy(first, buf, func(_ []int) bool {
		return t.enumerateChildrenLazy(rest, buf, emit)
	})
}

// ValidCount returns the number of valid permutations represented by the tree.
//
// ValidCount efficiently computes the count without enumerating all permutations.
// The count reflects all constraints applied via Reduce.
//
// The count is computed from the tree structure:
//   - P-nodes multiply by n! (factorial of child count)
//   - Q-nodes multiply by 2 (forward and reverse)
//   - Leaf nodes contribute 1
//
// For large trees, the count may be accurate even when Enumerate(0) would
// exhaust memory. Use ValidCount to check if enumeration is feasible before
// calling Enumerate without a limit.
//
// Example:
//
//	tree := perm.NewPQTree(5)           // 5! = 120 permutations
//	tree.Reduce([]int{1, 2, 3})
//	fmt.Println(tree.ValidCount())      // Much less than 120
func (t *PQTree) ValidCount() int {
	if t.root == nil {
		return 1
	}
	return t.countPerms(t.root)
}

func (t *PQTree) countPerms(node *pqNode) int {
	if node.kind == leafNode {
		return 1
	}

	product := 1
	for _, child := range node.children {
		product *= t.countPerms(child)
	}

	switch node.kind {
	case qNode:
		return 2 * product
	default:
		return Factorial(len(node.children)) * product
	}
}

// String returns a human-readable representation of the tree structure.
//
// The representation uses a nested notation:
//   - P-nodes (permutable): enclosed in curly braces {}
//   - Q-nodes (sequence): enclosed in square brackets []
//   - Leaf nodes: shown as single digits 0-9, or (a), (b), ... for indices >= 10
//
// String is equivalent to StringWithLabels(nil).
//
// Example output: "{0 {1 2 3} 4}" represents a tree where elements 1, 2, 3
// must be consecutive but can permute among themselves.
func (t *PQTree) String() string {
	return t.StringWithLabels(nil)
}

// StringWithLabels returns a human-readable representation using custom labels.
//
// The labels slice maps element indices to strings. If labels[i] exists, element i
// is displayed as labels[i] instead of its numeric index. This is useful for
// showing meaningful names in debugging output or examples.
//
// If labels is nil or shorter than needed, numeric indices are used as fallback.
// The labels slice is not modified.
//
// Example:
//
//	tree := perm.NewPQTree(3)
//	labels := []string{"app", "auth", "db"}
//	tree.Reduce([]int{0, 1})
//	fmt.Println(tree.StringWithLabels(labels))  // "{app auth} db" (or similar)
func (t *PQTree) StringWithLabels(labels []string) string {
	if t.root == nil {
		return "(empty)"
	}
	return t.nodeString(t.root, labels)
}

func (t *PQTree) nodeString(n *pqNode, labels []string) string {
	if n.kind == leafNode {
		switch {
		case n.value < len(labels):
			return labels[n.value]
		case n.value < 10:
			return string('0' + rune(n.value))
		default:
			return "(" + string('a'+rune(n.value-10)) + ")"
		}
	}

	open, close := "{", "}"
	if n.kind == qNode {
		open, close = "[", "]"
	}

	parts := make([]string, len(n.children))
	for i, child := range n.children {
		parts[i] = t.nodeString(child, labels)
	}
	return open + strings.Join(parts, " ") + close
}
