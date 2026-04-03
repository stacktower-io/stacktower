package deps

import (
	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// ShallowGraphFromDeps creates a shallow dependency graph with only direct dependencies.
// The graph has a virtual project root (ProjectRootNodeID) connected to each dependency.
// This is the standard pattern for manifest parsers that don't resolve transitive dependencies.
func ShallowGraphFromDeps(dependencies []Dependency) *dag.DAG {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: ProjectRootNodeID, Meta: dag.Metadata{"virtual": true}})
	for _, dep := range dependencies {
		_ = g.AddNode(dag.Node{ID: dep.Name})
		edgeMeta := dag.Metadata{}
		if dep.Constraint != "" {
			edgeMeta["constraint"] = dep.Constraint
		}
		_ = g.AddEdge(dag.Edge{From: ProjectRootNodeID, To: dep.Name, Meta: edgeMeta})
	}
	return g
}
