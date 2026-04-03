package graph

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/feature"
)

// =============================================================================
// Constants - Single Source of Truth
// =============================================================================

// Visualization types.
const (
	VizTypeTower    = "tower"
	VizTypeNodelink = "nodelink"
)

// Visual styles for rendering.
const (
	StyleSimple    = "simple"
	StyleHanddrawn = "handdrawn"
)

// ProjectRootNodeID is the node ID used for the root of manifest-based graphs.
const ProjectRootNodeID = "__project__"

// Node kinds.
const (
	KindSubdivider = "subdivider"
	KindAuxiliary  = "auxiliary"
)

// Internal metadata keys for serialization.
const (
	metaLabel    = "_label"   // Stores display label for round-trip fidelity
	metaRepoURL  = "repo_url" // Repository URL extraction
	metaHomePage = "homepage" // Homepage URL fallback when no repo_url
)

// =============================================================================
// Graph - Dependency Graph Serialization
// =============================================================================

// Graph is the canonical serialization format for dependency graphs.
// Used for API responses, storage, caching, and cross-tool compatibility.
//
// The format is human-readable and designed for round-trip fidelity:
// import → transform → export → re-import produces identical results.
type Graph struct {
	Meta  map[string]any `json:"meta,omitempty" bson:"meta,omitempty"` // Graph-level metadata (runtime version, dependency scope, etc.)
	Nodes []Node         `json:"nodes" bson:"nodes"`
	Edges []Edge         `json:"edges" bson:"edges"`
}

// =============================================================================
// Node - Unified Node Type
// =============================================================================

// Node is the unified node type for all serialization contexts.
// Used in both Graph and Layout types for consistency.
type Node struct {
	ID           string         `json:"id" bson:"id"`
	Label        string         `json:"label,omitempty" bson:"label,omitempty"`                 // Display label (defaults to ID)
	Row          int            `json:"row,omitempty" bson:"row,omitempty"`                     // Layer/rank assignment
	Kind         string         `json:"kind,omitempty" bson:"kind,omitempty"`                   // "subdivider", "auxiliary", or empty
	Brittle      bool           `json:"brittle,omitempty" bson:"brittle,omitempty"`             // At-risk package flag
	VulnSeverity string         `json:"vuln_severity,omitempty" bson:"vuln_severity,omitempty"` // Max vulnerability severity ("critical","high","medium","low")
	LicenseRisk  string         `json:"license_risk,omitempty" bson:"license_risk,omitempty"`   // License risk classification ("copyleft","weak-copyleft","unknown","proprietary")
	License      string         `json:"license,omitempty" bson:"license,omitempty"`             // License identifier/SPDX (e.g., "MIT", "Apache-2.0")
	LicenseText  string         `json:"license_text,omitempty" bson:"license_text,omitempty"`   // Full license text for custom/non-standard licenses (for LLM analysis)
	MasterID     string         `json:"master_id,omitempty" bson:"master_id,omitempty"`
	URL          string         `json:"url,omitempty" bson:"url,omitempty"` // Repository URL
	Meta         map[string]any `json:"meta,omitempty" bson:"meta,omitempty"`
}

// IsSubdivider returns true if this is a subdivider node.
func (n *Node) IsSubdivider() bool { return n.Kind == KindSubdivider }

// IsAuxiliary returns true if this is an auxiliary dependency.
func (n *Node) IsAuxiliary() bool { return n.Kind == KindAuxiliary }

// DisplayLabel returns the label if set, otherwise the ID.
func (n *Node) DisplayLabel() string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
}

// =============================================================================
// Edge - Directed Dependency
// =============================================================================

// Edge represents a directed edge in the dependency graph.
type Edge struct {
	From       string `json:"from" bson:"from"`
	To         string `json:"to" bson:"to"`
	Constraint string `json:"constraint,omitempty" bson:"constraint,omitempty"` // Version constraint (e.g., "^4.17.0", ">=2.0")
}

// =============================================================================
// DAG ↔ Graph Conversion
// =============================================================================

// FromDAG converts a DAG to its serialization format.
// Nodes are sorted by ID for deterministic output.
// Extracts repository URL and computes brittle flag from metadata.
func FromDAG(g *dag.DAG) Graph {
	nodes := g.Nodes()
	slices.SortFunc(nodes, func(a, b *dag.Node) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})

	out := Graph{
		Nodes: make([]Node, len(nodes)),
		Edges: make([]Edge, len(g.EdgesIter())),
	}

	// Include graph-level metadata if present
	if meta := g.Meta(); len(meta) > 0 {
		out.Meta = make(map[string]any, len(meta))
		for k, v := range meta {
			out.Meta[k] = v
		}
	}

	for i, n := range nodes {
		out.Nodes[i] = nodeFromDAG(n)
	}

	for i, e := range g.EdgesIter() {
		out.Edges[i] = edgeFromDAG(&e)
	}

	return out
}

// edgeFromDAG converts a dag.Edge to a serialization Edge.
// Extracts constraint from edge metadata if present.
func edgeFromDAG(e *dag.Edge) Edge {
	edge := Edge{From: e.From, To: e.To}
	if e.Meta != nil {
		if constraint, ok := e.Meta["constraint"].(string); ok {
			edge.Constraint = constraint
		}
	}
	return edge
}

// ToDAG converts a Graph to a DAG.
// Returns an error if the structure violates DAG constraints.
// Label is stored in metadata for round-trip fidelity when non-empty.
// Constraint is stored in edge metadata for round-trip fidelity when non-empty.
func ToDAG(gj Graph) (*dag.DAG, error) {
	d := dag.New(nil)

	for _, nj := range gj.Nodes {
		n := dag.Node{
			ID:       nj.ID,
			Row:      nj.Row,
			Meta:     copyMeta(nj.Meta),
			Kind:     stringToDAGKind(nj.Kind),
			MasterID: nj.MasterID,
		}
		if n.Meta == nil {
			n.Meta = dag.Metadata{}
		}
		// Store label in metadata for round-trip fidelity
		if nj.Label != "" {
			n.Meta[metaLabel] = nj.Label
		}
		// Store license data in metadata for round-trip fidelity
		if nj.License != "" {
			n.Meta["license"] = nj.License
		}
		if nj.LicenseText != "" {
			n.Meta["license_text"] = nj.LicenseText
		}
		if nj.LicenseRisk != "" {
			n.Meta["license_risk"] = nj.LicenseRisk
		}
		if err := d.AddNode(n); err != nil {
			return nil, fmt.Errorf("add node %s: %w", nj.ID, err)
		}
	}

	for _, ej := range gj.Edges {
		edge := dag.Edge{From: ej.From, To: ej.To}
		// Store constraint in edge metadata for round-trip fidelity
		if ej.Constraint != "" {
			edge.Meta = dag.Metadata{"constraint": ej.Constraint}
		}
		if err := d.AddEdge(edge); err != nil {
			return nil, fmt.Errorf("add edge %s→%s: %w", ej.From, ej.To, err)
		}
	}

	// Restore graph-level metadata
	if gj.Meta != nil {
		for k, v := range gj.Meta {
			d.Meta()[k] = v
		}
	}

	return d, nil
}

// copyMeta creates a shallow copy of metadata to avoid mutation.
func copyMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// UnmarshalGraph deserializes JSON bytes to a Graph.
func UnmarshalGraph(data []byte) (Graph, error) {
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return Graph{}, err
	}
	return g, nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// nodeFromDAG converts a dag.Node to a serialization Node.
// This is the single point of conversion for all DAG→Node operations.
// Label is preserved from metadata if previously stored (for round-trip fidelity).
// Brittle is computed from node metadata using feature.IsBrittle().
func nodeFromDAG(n *dag.Node) Node {
	node := Node{
		ID:       n.ID,
		Row:      n.Row,
		MasterID: n.MasterID,
		Meta:     cleanMeta(n.Meta),
		Kind:     dagKindToString(n.Kind),
		Brittle:  feature.IsBrittle(n),
	}

	// Extract fields from metadata
	if n.Meta != nil {
		// Prefer repo_url (GitHub), fallback to homepage for packages without repos
		if url, ok := n.Meta[metaRepoURL].(string); ok && url != "" {
			node.URL = url
		} else if hp, ok := n.Meta[metaHomePage].(string); ok && hp != "" {
			node.URL = hp
		}
		// Restore label for round-trip fidelity
		if label, ok := n.Meta[metaLabel].(string); ok {
			node.Label = label
		}
		// Propagate vulnerability severity (key matches security.MetaVulnSeverity)
		if vs, ok := n.Meta["vuln_severity"].(string); ok {
			node.VulnSeverity = vs
		}
		// Propagate license risk (key matches security.MetaLicenseRisk)
		if lr, ok := n.Meta["license_risk"].(string); ok && lr != "permissive" {
			node.LicenseRisk = lr
		}
		// Propagate license identifier (key matches security.MetaLicense)
		if lic, ok := n.Meta["license"].(string); ok {
			node.License = lic
		}
		// Propagate license text for custom/non-standard licenses (key matches security.MetaLicenseText)
		// Only include if license_risk indicates it needs review (proprietary/unknown)
		if licText, ok := n.Meta["license_text"].(string); ok && licText != "" {
			// Only include text for non-standard licenses to keep payloads manageable
			if node.LicenseRisk == "proprietary" || node.LicenseRisk == "unknown" || node.LicenseRisk == "copyleft" {
				node.LicenseText = licText
			}
		}
	}

	return node
}

// cleanMeta returns a copy of metadata without internal keys (e.g., _label).
// Returns nil if the result would be empty.
func cleanMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	// Check if we have any keys to preserve
	hasPublicKeys := false
	for k := range m {
		if k != metaLabel {
			hasPublicKeys = true
			break
		}
	}
	if !hasPublicKeys {
		return nil
	}
	// Copy without internal keys
	result := make(map[string]any, len(m))
	for k, v := range m {
		if k != metaLabel {
			result[k] = v
		}
	}
	return result
}

func dagKindToString(k dag.NodeKind) string {
	switch k {
	case dag.NodeKindSubdivider:
		return KindSubdivider
	case dag.NodeKindAuxiliary:
		return KindAuxiliary
	default:
		return ""
	}
}

func stringToDAGKind(s string) dag.NodeKind {
	switch s {
	case KindSubdivider:
		return dag.NodeKindSubdivider
	case KindAuxiliary:
		return dag.NodeKindAuxiliary
	default:
		return dag.NodeKindRegular
	}
}
