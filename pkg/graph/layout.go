package graph

import (
	"encoding/json"
	"fmt"
	"os"
)

// =============================================================================
// Layout - Unified Visualization Format
// =============================================================================

// Layout is the unified serialization format for all visualizations.
//
// This is a discriminated union type - check VizType to determine which
// fields are populated:
//
//	Tower ("tower"):
//	  - Blocks: positioned blocks with coordinates
//	  - MarginX/Y, Seed, Randomize, Merged: tower-specific render options
//
//	Nodelink ("nodelink"):
//	  - DOT: Graphviz DOT string for rendering
//	  - Engine: Graphviz layout engine (e.g., "dot")
//
// Shared fields (both types):
//   - Width, Height: frame dimensions
//   - Style: visual style ("handdrawn", "simple")
//   - Nodes: structured node metadata
//   - Edges: dependency edges
//   - Rows: layer assignments (row → node IDs)
//   - Nebraska: maintainer rankings (optional)
//
// For tower layouts, there is also an internal representation
// (pkg/core/render/tower/layout.Layout) optimized for computation.
// Use Export()/Parse() methods to convert between them.
type Layout struct {
	// Discriminator
	VizType string `json:"viz_type" bson:"viz_type"`

	// Common dimensions and style
	Width  float64 `json:"width" bson:"width"`
	Height float64 `json:"height" bson:"height"`
	Style  string  `json:"style,omitempty" bson:"style,omitempty"`

	// Graph structure (shared)
	Nodes    []Node            `json:"nodes,omitempty" bson:"nodes,omitempty"`
	Edges    []Edge            `json:"edges,omitempty" bson:"edges,omitempty"`
	Rows     map[int][]string  `json:"rows,omitempty" bson:"rows,omitempty"`
	Nebraska []NebraskaRanking `json:"nebraska,omitempty" bson:"nebraska,omitempty"`

	// Tower-specific
	Blocks    []Block `json:"blocks,omitempty" bson:"blocks,omitempty"`
	MarginX   float64 `json:"margin_x,omitempty" bson:"margin_x,omitempty"`
	MarginY   float64 `json:"margin_y,omitempty" bson:"margin_y,omitempty"`
	Seed      uint64  `json:"seed,omitempty" bson:"seed,omitempty"`
	Randomize bool    `json:"randomize,omitempty" bson:"randomize,omitempty"`
	Merged    bool    `json:"merged,omitempty" bson:"merged,omitempty"`

	// Nodelink-specific
	DOT    string `json:"dot,omitempty" bson:"dot,omitempty"`
	Engine string `json:"engine,omitempty" bson:"engine,omitempty"`
}

// IsTower returns true if this is a tower layout.
func (l *Layout) IsTower() bool { return l.VizType == VizTypeTower }

// IsNodelink returns true if this is a nodelink layout.
func (l *Layout) IsNodelink() bool { return l.VizType == VizTypeNodelink }

// =============================================================================
// Block - Tower Visualization Element
// =============================================================================

// Block represents a positioned block in a tower layout.
type Block struct {
	ID     string  `json:"id" bson:"id"`
	Label  string  `json:"label" bson:"label"`
	X      float64 `json:"x" bson:"x"`
	Y      float64 `json:"y" bson:"y"`
	Width  float64 `json:"width" bson:"width"`
	Height float64 `json:"height" bson:"height"`

	// Metadata
	URL          string     `json:"url,omitempty" bson:"url,omitempty"`
	Brittle      bool       `json:"brittle,omitempty" bson:"brittle,omitempty"`
	VulnSeverity string     `json:"vuln_severity,omitempty" bson:"vuln_severity,omitempty"` // Max vulnerability severity
	Auxiliary    bool       `json:"auxiliary,omitempty" bson:"auxiliary,omitempty"`
	Synthetic    bool       `json:"synthetic,omitempty" bson:"synthetic,omitempty"`
	Meta         *BlockMeta `json:"meta,omitempty" bson:"meta,omitempty"`
}

// BlockMeta contains enriched metadata (from GitHub, registries).
type BlockMeta struct {
	Description string `json:"description,omitempty" bson:"description,omitempty"`
	Stars       int    `json:"stars,omitempty" bson:"stars,omitempty"`
	LastCommit  string `json:"last_commit,omitempty" bson:"last_commit,omitempty"`
	LastRelease string `json:"last_release,omitempty" bson:"last_release,omitempty"`
	Archived    bool   `json:"archived,omitempty" bson:"archived,omitempty"`
}

// =============================================================================
// Nebraska - Maintainer Rankings
// =============================================================================

// NebraskaRanking contains maintainer ranking information.
type NebraskaRanking struct {
	Maintainer string            `json:"maintainer" bson:"maintainer"`
	Score      float64           `json:"score" bson:"score"`
	Packages   []NebraskaPackage `json:"packages" bson:"packages"`
}

// NebraskaPackage represents a package maintained by someone.
type NebraskaPackage struct {
	Package string `json:"package" bson:"package"`
	Role    string `json:"role" bson:"role"` // "owner", "lead", "maintainer"
	URL     string `json:"url,omitempty" bson:"url,omitempty"`
}

// =============================================================================
// Layout Serialization API
// =============================================================================

// MarshalLayout serializes a Layout to pretty-printed JSON bytes.
func MarshalLayout(l Layout) ([]byte, error) {
	return json.MarshalIndent(l, "", "  ")
}

// UnmarshalLayout deserializes JSON bytes into a Layout.
// Validates that required fields are present for the viz type.
func UnmarshalLayout(data []byte) (Layout, error) {
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return Layout{}, fmt.Errorf("unmarshal layout: %w", err)
	}

	if l.VizType == "" {
		l.VizType = VizTypeTower
	}

	if l.IsTower() && len(l.Blocks) == 0 {
		return Layout{}, fmt.Errorf("tower layout must contain blocks")
	}
	if l.IsNodelink() && l.DOT == "" {
		return Layout{}, fmt.Errorf("nodelink layout must contain DOT string")
	}

	return l, nil
}

// WriteLayoutFile writes a Layout to a JSON file.
func WriteLayoutFile(l Layout, path string) error {
	data, err := MarshalLayout(l)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ReadLayoutFile reads a Layout from a JSON file.
func ReadLayoutFile(path string) (Layout, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Layout{}, fmt.Errorf("read %s: %w", path, err)
	}
	return UnmarshalLayout(data)
}
