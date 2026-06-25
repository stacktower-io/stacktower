package transform

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// genReductionDAG builds a deterministic DAG with adjacent-layer edges plus
// redundant skip-level edges (the ones transitive reduction removes).
func genReductionDAG(tb testing.TB, n int, seed int64) *dag.DAG {
	tb.Helper()
	rng := rand.New(rand.NewSource(seed))
	g := dag.New(nil)

	var layers [][]string
	layers = append(layers, []string{"root"})
	if err := g.AddNode(dag.Node{ID: "root", Row: 0}); err != nil {
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
			if err := g.AddNode(dag.Node{ID: id, Row: row}); err != nil {
				tb.Fatal(err)
			}
			layer = append(layer, id)
			count++
		}
		layers = append(layers, layer)
	}

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
				_ = g.AddEdge(dag.Edge{From: prev[pi], To: id})
			}
		}
		// Redundant skip edges from two rows up (~30% of nodes).
		if row >= 2 {
			above := layers[row-2]
			for _, id := range layers[row] {
				if rng.Float64() < 0.3 {
					_ = g.AddEdge(dag.Edge{From: above[rng.Intn(len(above))], To: id})
				}
			}
		}
	}
	return g
}

func BenchmarkTransitiveReduction(b *testing.B) {
	for _, size := range []int{200, 2000} {
		b.Run(fmt.Sprintf("n%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				g := genReductionDAG(b, size, 42)
				b.StartTimer()
				TransitiveReduction(g)
			}
		})
	}
}
