package transform

// TransformResult contains metrics about transformations applied to a DAG.
//
// TransformResult is returned (alongside an error) by [Normalize] and
// [NormalizeWithOptions] to provide visibility into what transformations
// occurred. This is useful for logging, debugging, and understanding graph
// complexity.
type TransformResult struct {
	// CyclesRemoved is the number of back-edges removed by cycle breaking.
	// Zero indicates the input was already acyclic.
	CyclesRemoved int

	// TransitiveEdgesRemoved is the number of redundant edges removed by
	// transitive reduction. Higher values indicate more redundancy in the
	// original dependency graph.
	TransitiveEdgesRemoved int

	// SubdividersAdded is the number of synthetic subdivider nodes inserted
	// to break long edges into single-row segments. Higher values indicate
	// deeper dependency chains.
	SubdividersAdded int

	// SeparatorsAdded is the number of auxiliary separator beam nodes inserted
	// to resolve impossible crossing patterns. Non-zero values indicate the
	// presence of tangle motifs (e.g., complete bipartite subgraphs).
	SeparatorsAdded int

	// MaxRow is the final depth (maximum row number) after all transformations.
	// This represents the height of the tower layout.
	MaxRow int
}

// NormalizeOptions configures which transformations are applied by
// [NormalizeWithOptions].
//
// The zero value applies all transformations (equivalent to calling [Normalize]).
type NormalizeOptions struct {
	// SkipCycleBreaking disables cycle detection and removal. Use only when
	// the input graph is guaranteed to be acyclic. If cycles exist and this
	// is true, subsequent transformations may behave incorrectly.
	SkipCycleBreaking bool

	// SkipTransitiveReduction disables removal of redundant edges. This
	// preserves all edges from the input but may result in cluttered
	// visualizations with impossible geometry in tower layouts.
	SkipTransitiveReduction bool

	// SkipSeparators disables insertion of separator beams for tangle motifs.
	// If true, the output may contain unavoidable edge crossings. Use this
	// when crossings are acceptable or when the graph structure guarantees
	// no overlaps.
	SkipSeparators bool
}
