package cli

import (
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/integrations/github"
)

func TestAnnotateGitHubRootNode(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "stacktower", Meta: dag.Metadata{}})
	info := &github.RepoInfo{
		Description: "Dependency visualizer",
		Stars:       1234,
		Language:    "Go",
		License:     "MIT",
		Topics:      []string{"dependencies", "visualization"},
		Archived:    false,
	}

	annotateGitHubRootNode(g, "stacktower", "matzehuels", "stacktower", info)

	n, ok := g.Node("stacktower")
	if !ok {
		t.Fatal("root node not found")
	}
	if got, _ := n.Meta[metadata.RepoDescription].(string); got != "Dependency visualizer" {
		t.Fatalf("repo_description = %q", got)
	}
	if got, _ := n.Meta[metadata.RepoStars].(int); got != 1234 {
		t.Fatalf("repo_stars = %d", got)
	}
	if got, _ := n.Meta[metadata.RepoURL].(string); got != "https://github.com/matzehuels/stacktower" {
		t.Fatalf("repo_url = %q", got)
	}
}

func TestAnnotateGitHubRootNode_FallbackToProjectRootID(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: graph.ProjectRootNodeID, Meta: dag.Metadata{}})
	info := &github.RepoInfo{Description: "desc"}

	annotateGitHubRootNode(g, "custom-root", "o", "r", info)

	n, ok := g.Node(graph.ProjectRootNodeID)
	if !ok {
		t.Fatal("project root node not found")
	}
	if got, _ := n.Meta[metadata.RepoDescription].(string); got != "desc" {
		t.Fatalf("repo_description = %q", got)
	}
}
