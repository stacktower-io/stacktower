package pipeline

import (
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// =============================================================================
// Resolution Result Types
// =============================================================================

// ResolveResult holds the complete resolution output.
type ResolveResult struct {
	RootName  string
	Entries   []ResolveEntry
	DirectCnt int
	TransCnt  int
}

// ResolveEntry holds resolution details for a single package.
type ResolveEntry struct {
	Package     string
	Version     string   // resolved/pinned version
	Constraints []string // version constraints that applied
	RequiredBy  []string // packages that required this dependency
	IsDirect    bool     // true if directly required by root
}

// =============================================================================
// Resolution JSON Output Types
// =============================================================================

// ResolveJSON is the JSON-serializable format for resolution output.
type ResolveJSON struct {
	Meta       ResolveMetaJSON     `json:"meta"`
	Root       string              `json:"root"`
	Direct     []ResolveDependency `json:"direct"`
	Transitive []ResolveDependency `json:"transitive"`
	Summary    ResolveSummaryJSON  `json:"summary"`
}

// ResolveMetaJSON holds metadata about the resolution.
type ResolveMetaJSON struct {
	RuntimeVersion    string `json:"runtime_version,omitempty"`
	RuntimeSource     string `json:"runtime_source,omitempty"`
	DependencyScope   string `json:"dependency_scope,omitempty"`
	IncludePrerelease bool   `json:"include_prerelease"`
}

// ResolveDependency represents a single dependency in JSON output.
type ResolveDependency struct {
	Package    string   `json:"package"`
	Resolved   string   `json:"resolved"`
	Constraint string   `json:"constraint"`
	RequiredBy []string `json:"required_by,omitempty"`
}

// ResolveSummaryJSON holds summary statistics.
type ResolveSummaryJSON struct {
	Total      int `json:"total"`
	Direct     int `json:"direct"`
	Transitive int `json:"transitive"`
}

// ToJSON converts a ResolveResult to its JSON-serializable form.
func (r ResolveResult) ToJSON(meta ResolveMetaJSON) ResolveJSON {
	direct := make([]ResolveDependency, 0)
	transitive := make([]ResolveDependency, 0)

	for _, e := range r.Entries {
		constraint := strings.Join(e.Constraints, ", ")
		dep := ResolveDependency{
			Package:    e.Package,
			Resolved:   e.Version,
			Constraint: constraint,
		}
		if e.IsDirect {
			direct = append(direct, dep)
		} else {
			dep.RequiredBy = e.RequiredBy
			transitive = append(transitive, dep)
		}
	}

	return ResolveJSON{
		Meta:       meta,
		Root:       r.RootName,
		Direct:     direct,
		Transitive: transitive,
		Summary: ResolveSummaryJSON{
			Total:      r.DirectCnt + r.TransCnt,
			Direct:     r.DirectCnt,
			Transitive: r.TransCnt,
		},
	}
}

// =============================================================================
// Build Resolution Result from DAG
// =============================================================================

// BuildResolveResult extracts resolution details from a DAG.
func BuildResolveResult(g *dag.DAG, rootID string) ResolveResult {
	result := ResolveResult{RootName: rootID}

	// Find direct dependencies (children of root)
	directDeps := make(map[string]bool)
	for _, child := range g.Children(rootID) {
		directDeps[child] = true
	}

	// Build constraint and requiredBy maps from edges
	constraints := make(map[string][]string) // pkg -> constraints
	requiredBy := make(map[string][]string)  // pkg -> parent packages

	for _, e := range g.Edges() {
		if e.To == rootID {
			continue
		}
		if c, ok := e.Meta["constraint"].(string); ok && c != "" {
			constraints[e.To] = appendUnique(constraints[e.To], c)
		}
		if e.From != rootID {
			requiredBy[e.To] = appendUnique(requiredBy[e.To], e.From)
		}
	}

	// Build entries for all non-root nodes
	var directEntries, transitiveEntries []ResolveEntry
	for _, n := range g.Nodes() {
		if n.ID == rootID {
			continue
		}
		// Skip synthetic nodes (subdividers, auxiliary, etc.)
		if n.IsSynthetic() {
			continue
		}

		version := ""
		if v, ok := n.Meta["version"].(string); ok {
			version = v
		}

		entry := ResolveEntry{
			Package:     n.ID,
			Version:     version,
			Constraints: constraints[n.ID],
			RequiredBy:  requiredBy[n.ID],
			IsDirect:    directDeps[n.ID],
		}

		if entry.IsDirect {
			directEntries = append(directEntries, entry)
		} else {
			transitiveEntries = append(transitiveEntries, entry)
		}
	}

	// Sort entries alphabetically within each group
	sortEntriesByPackage(directEntries)
	sortEntriesByPackage(transitiveEntries)

	result.Entries = append(directEntries, transitiveEntries...)
	result.DirectCnt = len(directEntries)
	result.TransCnt = len(transitiveEntries)

	return result
}

// appendUnique appends a string to a slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// sortEntriesByPackage sorts resolve entries alphabetically by package name.
func sortEntriesByPackage(entries []ResolveEntry) {
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].Package > entries[j].Package {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}
