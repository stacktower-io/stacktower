package deps

import (
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

func TestFilterPrereleaseNodes_ExcludesPrerelease(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root"})
	_ = g.AddNode(dag.Node{ID: "stable", Meta: dag.Metadata{"version": "1.2.3"}})
	_ = g.AddNode(dag.Node{ID: "pre", Meta: dag.Metadata{"version": "2.0.0-rc.1"}})
	_ = g.AddEdge(dag.Edge{From: "root", To: "stable"})
	_ = g.AddEdge(dag.Edge{From: "root", To: "pre"})

	filtered := FilterPrereleaseNodes(g, false)
	if _, ok := filtered.Node("pre"); ok {
		t.Fatalf("expected prerelease dependency to be filtered out")
	}
	if _, ok := filtered.Node("stable"); !ok {
		t.Fatalf("expected stable dependency to remain")
	}
}

func TestFilterPrereleaseNodes_IncludePrereleaseKeepsNodes(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root"})
	_ = g.AddNode(dag.Node{ID: "pre", Meta: dag.Metadata{"version": "1.0.0-beta.2"}})
	_ = g.AddEdge(dag.Edge{From: "root", To: "pre"})

	filtered := FilterPrereleaseNodes(g, true)
	if _, ok := filtered.Node("pre"); !ok {
		t.Fatalf("expected prerelease dependency to remain when include-prerelease is true")
	}
}

func TestFilterPrereleaseNodes_IncludesAllWhenTrue(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root"})
	_ = g.AddNode(dag.Node{ID: "golang.org/x/exp"})
	_ = g.AddEdge(dag.Edge{From: "root", To: "golang.org/x/exp"})

	filtered := FilterPrereleaseNodes(g, true)
	if _, ok := filtered.Node("golang.org/x/exp"); !ok {
		t.Fatalf("expected all nodes to remain when include-prerelease is true")
	}
}

func TestIsPrereleaseVersion_GoPseudoVersionNotPrerelease(t *testing.T) {
	if IsPrereleaseVersion("v0.0.0-20260218203240-3dfff04db8fa") {
		t.Fatalf("go pseudo versions must not be treated as prerelease")
	}
	if !IsPrereleaseVersion("1.0.0-rc.1") {
		t.Fatalf("expected rc marker to be treated as prerelease")
	}
}

func TestIsPrereleaseVersion_PEP440AbbreviatedMarkers(t *testing.T) {
	tests := []struct {
		version    string
		prerelease bool
	}{
		// PEP 440 alpha (a)
		{"1.0.0a1", true},
		{"2.13.0a2", true},
		// PEP 440 beta (b)
		{"1.0.0b1", true},
		{"2.13.0b1", true},
		{"2.13.0b12", true},
		// Stable versions (should NOT be detected as prerelease)
		{"1.0.0", false},
		{"2.12.5", false},
		{"0.52.1", false},
		// Post releases are stable
		{"1.0.0.post1", false},
		// Version with 'a' or 'b' not in prerelease context
		{"1.0.0ab1", false}, // 'ab' is not 'a' followed by digit
		{"2.4", false},      // just ends with digit, no prerelease
	}

	for _, tt := range tests {
		got := IsPrereleaseVersion(tt.version)
		if got != tt.prerelease {
			t.Errorf("IsPrereleaseVersion(%q) = %v, want %v", tt.version, got, tt.prerelease)
		}
	}
}

func TestIsPrereleaseVersion_MavenMilestones(t *testing.T) {
	tests := []struct {
		version    string
		prerelease bool
	}{
		// Maven milestone releases
		{"7.0.0-M6", true},
		{"7.0.0-M5", true},
		{"3.0.0-M1", true},
		{"1.0.0-M10", true},
		// Full "milestone" word
		{"2.0.0-milestone.1", true},
		// Stable versions (should NOT be detected as prerelease)
		{"6.2.16", false},
		{"7.0.0", false},
		// Versions with M not in milestone context
		{"1.0.0-MUSL", false}, // -M not followed by digit
	}

	for _, tt := range tests {
		got := IsPrereleaseVersion(tt.version)
		if got != tt.prerelease {
			t.Errorf("IsPrereleaseVersion(%q) = %v, want %v", tt.version, got, tt.prerelease)
		}
	}
}
