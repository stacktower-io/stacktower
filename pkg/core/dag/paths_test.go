package dag

import (
	"slices"
	"testing"
)

func buildPathTestGraph() *DAG {
	//   A
	//  / \
	// B   C
	//  \ / \
	//   D   E
	//   |
	//   F
	g := New(nil)
	for _, n := range []struct{ id string }{{"A"}, {"B"}, {"C"}, {"D"}, {"E"}, {"F"}} {
		g.AddNode(Node{ID: n.id, Row: 0})
	}
	for _, e := range [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}, {"C", "E"}, {"D", "F"}} {
		g.AddEdge(Edge{From: e[0], To: e[1]})
	}
	return g
}

func sortPaths(paths [][]string) {
	slices.SortFunc(paths, func(a, b []string) int {
		for i := range min(len(a), len(b)) {
			if a[i] < b[i] {
				return -1
			}
			if a[i] > b[i] {
				return 1
			}
		}
		return len(a) - len(b)
	})
}

func TestFindPaths_SinglePath(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "E", 0)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	want := []string{"A", "C", "E"}
	if !slices.Equal(paths[0], want) {
		t.Errorf("got %v, want %v", paths[0], want)
	}
}

func TestFindPaths_MultiplePaths(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "D", 0)
	sortPaths(paths)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	want := [][]string{
		{"A", "B", "D"},
		{"A", "C", "D"},
	}
	for i, p := range paths {
		if !slices.Equal(p, want[i]) {
			t.Errorf("path %d: got %v, want %v", i, p, want[i])
		}
	}
}

func TestFindPaths_NoPath(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "B", "C", 0)
	if len(paths) != 0 {
		t.Errorf("expected no paths, got %d", len(paths))
	}
}

func TestFindPaths_TargetNotInGraph(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "Z", 0)
	if paths != nil {
		t.Errorf("expected nil, got %v", paths)
	}
}

func TestFindPaths_TargetIsRoot(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "A", 0)
	if len(paths) != 1 || len(paths[0]) != 1 || paths[0][0] != "A" {
		t.Errorf("expected [[A]], got %v", paths)
	}
}

func TestFindPaths_Diamond(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "F", 0)
	sortPaths(paths)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths through diamond, got %d: %v", len(paths), paths)
	}
	want := [][]string{
		{"A", "B", "D", "F"},
		{"A", "C", "D", "F"},
	}
	for i, p := range paths {
		if !slices.Equal(p, want[i]) {
			t.Errorf("path %d: got %v, want %v", i, p, want[i])
		}
	}
}

func TestFindPaths_MaxPaths(t *testing.T) {
	g := buildPathTestGraph()
	paths := FindPaths(g, "A", "F", 1)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path with maxPaths=1, got %d", len(paths))
	}
}

func TestShortestPaths(t *testing.T) {
	//   R
	//  / \
	// A   B
	// |   |
	// C   |
	//  \ /
	//   T
	g := New(nil)
	for _, id := range []string{"R", "A", "B", "C", "T"} {
		g.AddNode(Node{ID: id, Row: 0})
	}
	g.AddEdge(Edge{From: "R", To: "A"})
	g.AddEdge(Edge{From: "R", To: "B"})
	g.AddEdge(Edge{From: "A", To: "C"})
	g.AddEdge(Edge{From: "C", To: "T"})
	g.AddEdge(Edge{From: "B", To: "T"})

	shortest := ShortestPaths(g, "R", "T")
	if len(shortest) != 1 {
		t.Fatalf("expected 1 shortest path, got %d: %v", len(shortest), shortest)
	}
	want := []string{"R", "B", "T"}
	if !slices.Equal(shortest[0], want) {
		t.Errorf("got %v, want %v", shortest[0], want)
	}
}

func TestShortestDepth(t *testing.T) {
	paths := [][]string{
		{"A", "B", "C", "D"},
		{"A", "C", "D"},
	}
	if d := ShortestDepth(paths); d != 2 {
		t.Errorf("expected depth 2, got %d", d)
	}
}

func TestShortestDepth_Empty(t *testing.T) {
	if d := ShortestDepth(nil); d != -1 {
		t.Errorf("expected -1, got %d", d)
	}
}
