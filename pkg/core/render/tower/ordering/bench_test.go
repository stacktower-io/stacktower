package ordering

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// genLayeredDAG builds a deterministic synthetic layered graph for ordering
// benchmarks and crossing-count golden tests.
func genLayeredDAG(tb testing.TB, n int, seed int64) *dag.DAG {
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
				if err := g.AddEdge(dag.Edge{From: prev[pi], To: id}); err != nil {
					tb.Fatal(err)
				}
			}
		}
	}
	return g
}

func BenchmarkBarycentric(b *testing.B) {
	for _, size := range []int{200, 2000} {
		g := genLayeredDAG(b, size, 42)
		b.Run(fmt.Sprintf("n%d", size), func(b *testing.B) {
			b.ReportAllocs()
			var crossings int
			for i := 0; i < b.N; i++ {
				orders := Barycentric{}.OrderRows(g)
				crossings = dag.CountCrossings(g, orders)
			}
			b.ReportMetric(float64(crossings), "crossings")
		})
	}
}

// TestBarycentric_CrossingGoldens pins the crossing counts produced on fixed
// synthetic graphs. Quality improvements should lower these numbers; any
// regression above the golden value fails.
func TestBarycentric_CrossingGoldens(t *testing.T) {
	goldens := []struct {
		n            int
		seed         int64
		maxCrossings int
	}{
		{200, 42, goldenCrossings200},
		{2000, 42, goldenCrossings2000},
		{500, 7, goldenCrossings500},
	}

	for _, tt := range goldens {
		t.Run(fmt.Sprintf("n%d_seed%d", tt.n, tt.seed), func(t *testing.T) {
			g := genLayeredDAG(t, tt.n, tt.seed)
			orders := Barycentric{}.OrderRows(g)
			got := dag.CountCrossings(g, orders)
			t.Logf("crossings: %d (golden max %d)", got, tt.maxCrossings)
			if got > tt.maxCrossings {
				t.Errorf("crossings = %d, exceeds golden max %d", got, tt.maxCrossings)
			}
		})
	}
}

// Golden crossing counts; tighten when ordering quality improves.
// Baseline before Phase 6 tuning: 638 / 33506 / 3098.
const (
	goldenCrossings200  = 537
	goldenCrossings2000 = 32421
	goldenCrossings500  = 2938
)
