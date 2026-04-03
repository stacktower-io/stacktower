package sink

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/core/deps/metadata"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/feature"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/layout"
	"github.com/matzehuels/stacktower/pkg/core/render/tower/styles"
	"github.com/matzehuels/stacktower/pkg/fonts"
	"github.com/matzehuels/stacktower/pkg/security"
)

const blockInteractionCSS = `
    .block { transition: stroke-width 0.2s ease; }
    .block.highlight { stroke-width: 4; }
    .block-text { transition: transform 0.2s ease; transform-origin: center; transform-box: fill-box; }
    .block-text.highlight { transform: scale(1.08); font-weight: bold; }
    a { cursor: pointer; }`

const blockInteractionJS = `
    function highlight(pkgs) {
      document.querySelectorAll('.block').forEach(b => b.classList.toggle('highlight', pkgs.includes(b.id.replace('block-', ''))));
      document.querySelectorAll('.block-text').forEach(t => t.classList.toggle('highlight', pkgs.includes(t.dataset.block)));
      document.querySelectorAll('.license-flag, .license-stripe, .vuln-flag').forEach(f => f.classList.toggle('highlight', pkgs.includes(f.dataset.block)));
    }
    function clearHighlight() {
      document.querySelectorAll('.block, .block-text, .license-flag, .license-stripe, .vuln-flag').forEach(el => el.classList.remove('highlight'));
    }
    document.querySelectorAll('.block').forEach(el => {
      el.addEventListener('mouseenter', () => highlight([el.id.replace('block-', '')]));
      el.addEventListener('mouseleave', clearHighlight);
    });
    document.querySelectorAll('.block-text').forEach(el => {
      el.addEventListener('mouseenter', () => highlight([el.dataset.block]));
      el.addEventListener('mouseleave', clearHighlight);
    });`

type SVGOption func(*svgRenderer)

type svgRenderer struct {
	graph      *dag.DAG
	style      styles.Style
	showEdges  bool
	merged     bool
	nebraska   []feature.NebraskaRanking
	popups     bool
	flagsOnTop bool
}

func WithGraph(g *dag.DAG) SVGOption     { return func(r *svgRenderer) { r.graph = g } }
func WithEdges() SVGOption               { return func(r *svgRenderer) { r.showEdges = true } }
func WithStyle(s styles.Style) SVGOption { return func(r *svgRenderer) { r.style = s } }
func WithMerged() SVGOption              { return func(r *svgRenderer) { r.merged = true } }
func WithNebraska(rankings []feature.NebraskaRanking) SVGOption {
	return func(r *svgRenderer) { r.nebraska = rankings }
}
func WithPopups() SVGOption { return func(r *svgRenderer) { r.popups = true } }

// WithFlagsOnTop controls whether security flags (license, vuln) are rendered
// in a separate pass after all blocks, ensuring they appear on top.
// Default is true. Set to false to render flags with their blocks.
func WithFlagsOnTop(on bool) SVGOption { return func(r *svgRenderer) { r.flagsOnTop = on } }

func RenderSVG(l layout.Layout, opts ...SVGOption) []byte {
	r := newSVGRenderer(opts...)

	blocks := buildBlocks(l, r.graph, r.popups)
	slices.SortFunc(blocks, func(a, b styles.Block) int {
		return cmp.Compare(a.ID, b.ID)
	})

	var edges []styles.Edge
	if r.showEdges {
		edges = buildEdges(l, r.graph, r.merged)
	}

	totalWidth, totalHeight := calculateDimensions(l, r.nebraska)

	// Pre-size buffer to reduce reallocations: ~500 bytes per block, ~100 per edge
	estimatedSize := len(blocks)*500 + len(edges)*100 + 8192
	buf := bytes.NewBuffer(make([]byte, 0, estimatedSize))
	fmt.Fprintf(buf, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.1f %.1f" width="%.0f" height="%.0f">`+"\n",
		totalWidth, totalHeight, totalWidth, totalHeight)

	r.style.RenderDefs(buf)
	renderContent(buf, &r, blocks, edges)
	renderBlockInteraction(buf)

	if len(r.nebraska) > 0 {
		renderNebraskaPanel(buf, l.FrameWidth, l.FrameHeight, r.nebraska)
		renderNebraskaScript(buf)
	}

	if r.popups {
		for _, b := range blocks {
			r.style.RenderPopup(buf, b)
		}
		renderPopupScript(buf)
	}

	// Add watermark
	renderWatermark(buf, l.FrameWidth)

	buf.WriteString("</svg>\n")
	return buf.Bytes()
}

func newSVGRenderer(opts ...SVGOption) svgRenderer {
	r := svgRenderer{
		style:      styles.Simple{},
		flagsOnTop: true, // Default: render flags on top of all blocks
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

const watermarkMargin = 40.0 // Space reserved at top for watermark

func calculateDimensions(l layout.Layout, nebraska []feature.NebraskaRanking) (width, height float64) {
	width = l.FrameWidth
	height = l.FrameHeight + watermarkMargin // Add space for watermark at top

	if len(nebraska) > 0 {
		isLandscape := l.FrameWidth > l.FrameHeight
		if isLandscape {
			// Panel on the right: add to width
			width += nebraskaPanelLandscape
		} else {
			// Panel below: add to height
			height += nebraskaPanelPortrait
		}
	}
	return width, height
}

func renderContent(buf *bytes.Buffer, r *svgRenderer, blocks []styles.Block, edges []styles.Edge) {
	// Shift all content down by watermark margin to make room at top
	fmt.Fprintf(buf, `  <g transform="translate(0, %.1f)">`+"\n", watermarkMargin)

	for _, b := range blocks {
		r.style.RenderBlock(buf, b)
		if !r.flagsOnTop {
			// Render flags inline with each block
			r.style.RenderFlags(buf, b)
		}
	}
	for _, e := range edges {
		r.style.RenderEdge(buf, e)
	}
	for _, b := range blocks {
		if shouldSkipText(r.graph, b.ID) {
			continue
		}
		r.style.RenderText(buf, b)
	}
	if r.flagsOnTop {
		// Render flags last so they always appear on top of all blocks
		for _, b := range blocks {
			r.style.RenderFlags(buf, b)
		}
	}

	buf.WriteString("  </g>\n")
}

func shouldSkipText(g *dag.DAG, id string) bool {
	if g == nil {
		return false
	}
	n, ok := g.Node(id)
	return ok && n.IsAuxiliary()
}

func renderBlockInteraction(buf *bytes.Buffer) {
	fmt.Fprintf(buf, "  <style>%s\n  </style>\n", blockInteractionCSS)
	fmt.Fprintf(buf, "  <script type=\"text/javascript\"><![CDATA[%s\n  ]]></script>\n", blockInteractionJS)
}

func buildBlocks(l layout.Layout, g *dag.DAG, withPopups bool) []styles.Block {
	blocks := make([]styles.Block, 0, len(l.Blocks))
	for id, b := range l.Blocks {
		blk := styles.Block{
			ID:    id,
			Label: b.NodeID,
			X:     b.Left, Y: b.Bottom,
			W: b.Width(), H: b.Height(),
			CX: b.CenterX(), CY: b.CenterY(),
		}
		if g != nil {
			if n, ok := g.Node(id); ok && n.Meta != nil {
				// Prefer repo_url (GitHub), fallback to homepage for packages without repos
				if url, ok := n.Meta[metadata.RepoURL].(string); ok && url != "" {
					blk.URL = url
				} else if hp, ok := n.Meta[metadata.HomePage].(string); ok && hp != "" {
					blk.URL = hp
				}
				blk.Brittle = feature.IsBrittle(n)
				if vs, ok := n.Meta[security.MetaVulnSeverity].(string); ok {
					blk.VulnSeverity = vs
				}
				if lic, ok := n.Meta["license"].(string); ok {
					blk.License = lic
				}
				if lr, ok := n.Meta[security.MetaLicenseRisk].(string); ok {
					blk.LicenseRisk = lr
				}
				if withPopups {
					blk.Popup = extractPopupData(n)
				}
			}
		}
		blocks = append(blocks, blk)
	}
	return blocks
}

func buildEdges(l layout.Layout, g *dag.DAG, merged bool) []styles.Edge {
	if g == nil {
		return nil
	}
	if merged {
		return buildMergedEdges(l, g)
	}
	return buildSimpleEdges(l, g)
}

func buildSimpleEdges(l layout.Layout, g *dag.DAG) []styles.Edge {
	edges := make([]styles.Edge, 0, len(g.EdgesIter()))
	for _, e := range g.EdgesIter() {
		src, okS := l.Blocks[e.From]
		dst, okD := l.Blocks[e.To]
		if !okS || !okD {
			continue
		}
		edges = append(edges, styles.Edge{
			FromID: e.From, ToID: e.To,
			X1: src.CenterX(), Y1: src.CenterY(),
			X2: dst.CenterX(), Y2: dst.CenterY(),
		})
	}
	return edges
}

func buildMergedEdges(l layout.Layout, g *dag.DAG) []styles.Edge {
	masterOf := func(id string) string {
		if n, ok := g.Node(id); ok && n.MasterID != "" {
			return n.MasterID
		}
		return id
	}

	blockFor := func(id string) (layout.Block, bool) {
		if b, ok := l.Blocks[id]; ok {
			return b, true
		}
		if master := masterOf(id); master != id {
			if b, ok := l.Blocks[master]; ok {
				return b, true
			}
		}
		return layout.Block{}, false
	}

	type edgeKey struct{ from, to string }
	seen := make(map[edgeKey]struct{})
	var edges []styles.Edge

	for _, e := range g.EdgesIter() {
		fromMaster, toMaster := masterOf(e.From), masterOf(e.To)
		if fromMaster == toMaster {
			continue
		}

		key := edgeKey{fromMaster, toMaster}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		src, okS := blockFor(e.From)
		dst, okD := blockFor(e.To)
		if !okS || !okD {
			continue
		}

		edges = append(edges, styles.Edge{
			FromID: fromMaster, ToID: toMaster,
			X1: src.CenterX(), Y1: src.CenterY(),
			X2: dst.CenterX(), Y2: dst.CenterY(),
		})
	}
	return edges
}

// renderWatermark adds a watermark with the Stacktower logo centered at the top
func renderWatermark(buf *bytes.Buffer, frameWidth float64) {
	// Center horizontally in the reserved watermark space at top
	x := (frameWidth - 120) / 2     // Center the watermark (icon + text width ~120)
	y := (watermarkMargin - 10) / 2 // Vertically center in the watermark margin space

	// Stacktower icon (Layers icon from favicon.svg, scaled to match text height)
	// Original viewBox is 0 0 24 24, we scale it to ~16x16 to match 14px text
	icon := `<g transform="scale(0.67)">
      <path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
      <path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
      <path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
    </g>`

	fmt.Fprintf(buf, `  <a href="https://www.stacktower.io" target="_blank" rel="noopener">
    <g transform="translate(%.1f, %.1f)" class="watermark">
      %s
      <text x="20" y="11" font-family="%s" font-size="14" font-weight="500" fill="#000">stacktower.io</text>
    </g>
  </a>
`, x, y, icon, fonts.FallbackFontFamily)

	// Add hover effect
	buf.WriteString(`  <style>
    .watermark { transition: opacity 0.2s ease; opacity: 1; }
    .watermark:hover { opacity: 0.7; }
  </style>
`)
}
