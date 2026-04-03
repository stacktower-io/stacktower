package layout

import (
	"fmt"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/feature"
	"github.com/matzehuels/stacktower/pkg/graph"
	"github.com/matzehuels/stacktower/pkg/security"
)

// Export converts an internal tower layout to the serialization format.
//
// Use this when you need to serialize the layout for:
//   - JSON file output (via graph.WriteLayoutFile)
//   - API responses
//   - Caching
//
// The DAG is optional but recommended for metadata enrichment (URLs, brittle flags, etc.).
func (l Layout) Export(g *dag.DAG) (graph.Layout, error) {
	result := graph.Layout{
		VizType:   graph.VizTypeTower,
		Width:     l.FrameWidth,
		Height:    l.FrameHeight,
		MarginX:   l.MarginX,
		MarginY:   l.MarginY,
		Style:     l.Style,
		Seed:      l.Seed,
		Randomize: l.Randomize,
		Merged:    l.Merged,
		Rows:      l.RowOrders,
		Blocks:    buildBlocks(l, g),
		Nodes:     buildNodes(g),
	}

	if g != nil {
		result.Edges = buildEdges(l, g, l.Merged)
	}

	if len(l.Nebraska) > 0 {
		result.Nebraska = buildNebraska(l.Nebraska)
	}

	return result, nil
}

// Parse converts a serialized layout to an internal tower layout.
//
// Use this when you need to render from a previously serialized layout:
//   - Loading from JSON file (via graph.ReadLayoutFile)
//   - Receiving from API/cache
//
// Returns an error if the layout is not a tower type (VizType must be "tower" or empty).
func Parse(layout graph.Layout) (Layout, error) {
	if layout.VizType != "" && layout.VizType != graph.VizTypeTower {
		return Layout{}, fmt.Errorf("invalid viz_type for tower layout: %q", layout.VizType)
	}

	l := Layout{
		FrameWidth:  layout.Width,
		FrameHeight: layout.Height,
		MarginX:     layout.MarginX,
		MarginY:     layout.MarginY,
		RowOrders:   layout.Rows,
		Blocks:      make(map[string]Block, len(layout.Blocks)),
		Style:       layout.Style,
		Seed:        layout.Seed,
		Randomize:   layout.Randomize,
		Merged:      layout.Merged,
		Nebraska:    parseNebraska(layout.Nebraska),
	}

	for _, b := range layout.Blocks {
		l.Blocks[b.ID] = Block{
			NodeID: b.Label,
			Left:   b.X,
			Right:  b.X + b.Width,
			Bottom: b.Y,
			Top:    b.Y + b.Height,
		}
	}

	return l, nil
}

// =============================================================================
// Block Building Helpers
// =============================================================================

func buildBlocks(l Layout, g *dag.DAG) []graph.Block {
	blocks := make([]graph.Block, 0, len(l.Blocks))
	for id, b := range l.Blocks {
		bd := graph.Block{
			ID:     id,
			Label:  b.NodeID,
			X:      b.Left,
			Y:      b.Bottom,
			Width:  b.Width(),
			Height: b.Height(),
		}
		if g != nil {
			if n, ok := g.Node(id); ok {
				bd.Auxiliary = n.IsAuxiliary()
				bd.Synthetic = n.IsSynthetic()
				if n.Meta != nil {
					bd.URL, _ = n.Meta[metadata.RepoURL].(string)
					bd.Brittle = feature.IsBrittle(n)
					if vs, ok := n.Meta[security.MetaVulnSeverity].(string); ok {
						bd.VulnSeverity = vs
					}
					bd.Meta = extractMeta(n)
				}
			}
		}
		blocks = append(blocks, bd)
	}
	return blocks
}

func buildNodes(g *dag.DAG) []graph.Node {
	if g == nil {
		return nil
	}
	serialized := graph.FromDAG(g)
	// Enrich nodes with computed brittle flag
	for i := range serialized.Nodes {
		if n, ok := g.Node(serialized.Nodes[i].ID); ok {
			serialized.Nodes[i].Brittle = feature.IsBrittle(n)
		}
	}
	return serialized.Nodes
}

func extractMeta(n *dag.Node) *graph.BlockMeta {
	if n.Meta == nil {
		return nil
	}
	m := &graph.BlockMeta{
		Stars: feature.AsInt(n.Meta[metadata.RepoStars]),
	}
	m.LastCommit, _ = n.Meta[metadata.RepoLastCommit].(string)
	m.LastRelease, _ = n.Meta[metadata.RepoLastRelease].(string)
	m.Archived, _ = n.Meta[metadata.RepoArchived].(bool)

	if desc, ok := n.Meta[metadata.RepoDescription].(string); ok && desc != "" {
		m.Description = desc
	}

	if m.Description == "" && m.Stars == 0 && m.LastCommit == "" && m.LastRelease == "" && !m.Archived {
		return nil
	}
	return m
}

// =============================================================================
// Edge Building Helpers
// =============================================================================

func buildEdges(l Layout, g *dag.DAG, merged bool) []graph.Edge {
	if merged {
		return buildMergedEdges(l, g)
	}
	edges := make([]graph.Edge, 0)
	for _, e := range g.Edges() {
		if _, ok := l.Blocks[e.From]; !ok {
			continue
		}
		if _, ok := l.Blocks[e.To]; !ok {
			continue
		}
		edges = append(edges, graph.Edge{From: e.From, To: e.To})
	}
	return edges
}

func buildMergedEdges(l Layout, g *dag.DAG) []graph.Edge {
	masterOf := func(id string) string {
		if n, ok := g.Node(id); ok && n.MasterID != "" {
			return n.MasterID
		}
		return id
	}

	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]struct{})
	var edges []graph.Edge

	for _, e := range g.Edges() {
		fromMaster, toMaster := masterOf(e.From), masterOf(e.To)
		if fromMaster == toMaster {
			continue
		}
		key := edgeKey{fromMaster, toMaster}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		edges = append(edges, graph.Edge{From: fromMaster, To: toMaster})
	}
	return edges
}

// =============================================================================
// Nebraska Helpers
// =============================================================================

func buildNebraska(rankings []feature.NebraskaRanking) []graph.NebraskaRanking {
	result := make([]graph.NebraskaRanking, len(rankings))
	for i, r := range rankings {
		pkgs := make([]graph.NebraskaPackage, len(r.Packages))
		for j, p := range r.Packages {
			pkgs[j] = graph.NebraskaPackage{Package: p.Package, Role: string(p.Role), URL: p.URL}
		}
		result[i] = graph.NebraskaRanking{
			Maintainer: r.Maintainer,
			Score:      r.Score,
			Packages:   pkgs,
		}
	}
	return result
}

func parseNebraska(data []graph.NebraskaRanking) []feature.NebraskaRanking {
	if len(data) == 0 {
		return nil
	}
	result := make([]feature.NebraskaRanking, len(data))
	for i, d := range data {
		pkgs := make([]feature.PackageRole, len(d.Packages))
		for j, p := range d.Packages {
			pkgs[j] = feature.PackageRole{
				Package: p.Package,
				Role:    feature.Role(p.Role),
				URL:     p.URL,
			}
		}
		result[i] = feature.NebraskaRanking{
			Maintainer: d.Maintainer,
			Score:      d.Score,
			Packages:   pkgs,
		}
	}
	return result
}
