package dag

import (
	"fmt"
	"math/rand"
	"testing"
)

// genLayeredDAG builds a deterministic synthetic dependency graph with
// approximately n nodes arranged in layers, a single root, and edges between
// adjacent layers. Used by benchmarks and crossing-count golden tests.
func genLayeredDAG(tb testing.TB, n int, seed int64) *DAG {
	tb.Helper()
	rng := rand.New(rand.NewSource(seed))
	g := New(nil)

	// Layer widths grow then shrink, mimicking real dependency stacks.
	var layers [][]string
	layers = append(layers, []string{"root"})
	if err := g.AddNode(Node{ID: "root", Row: 0}); err != nil {
		tb.Fatal(err)
	}
	count := 1
	for row := 1; count < n; row++ {
		width := 2 + rng.Intn(8+row)
		if width > n-count {
			width = n - count
		}
		layer := make([]string, 0, width)
		for i := 0; i < width; i++ {
			id := fmt.Sprintf("n%d_%d", row, i)
			if err := g.AddNode(Node{ID: id, Row: row}); err != nil {
				tb.Fatal(err)
			}
			layer = append(layer, id)
			count++
		}
		layers = append(layers, layer)
	}

	// Connect every node to 1-3 parents in the previous layer.
	for row := 1; row < len(layers); row++ {
		prev := layers[row-1]
		for _, id := range layers[row] {
			parents := 1 + rng.Intn(3)
			seen := map[int]bool{}
			for p := 0; p < parents; p++ {
				pi := rng.Intn(len(prev))
				if seen[pi] {
					continue
				}
				seen[pi] = true
				if err := g.AddEdge(Edge{From: prev[pi], To: id}); err != nil {
					tb.Fatal(err)
				}
			}
		}
	}
	return g
}

func BenchmarkComputeStats(b *testing.B) {
	for _, size := range []int{200, 2000} {
		g := genLayeredDAG(b, size, 42)
		b.Run(fmt.Sprintf("n%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ComputeStats(g)
			}
		})
	}
}

func BenchmarkCountCrossings(b *testing.B) {
	for _, size := range []int{200, 2000} {
		g := genLayeredDAG(b, size, 42)
		orders := make(map[int][]string)
		for _, r := range g.RowIDs() {
			orders[r] = NodeIDs(g.NodesInRow(r))
		}
		b.Run(fmt.Sprintf("n%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				CountCrossings(g, orders)
			}
		})
	}
}
