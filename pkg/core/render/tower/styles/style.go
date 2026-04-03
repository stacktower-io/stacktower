package styles

import "bytes"

// Style defines the visual appearance for tower rendering.
// Implementations control how blocks, edges, text, and popups are drawn.
type Style interface {
	// RenderDefs writes SVG <defs> content (filters, patterns, gradients).
	RenderDefs(buf *bytes.Buffer)
	// RenderBlock writes the SVG for a single block shape.
	RenderBlock(buf *bytes.Buffer, b Block)
	// RenderFlags writes the SVG for a block's security flags (license, vuln).
	// Flags are rendered separately so they can be drawn on top of all blocks.
	RenderFlags(buf *bytes.Buffer, b Block)
	// RenderEdge writes the SVG for a dependency edge line.
	RenderEdge(buf *bytes.Buffer, e Edge)
	// RenderText writes the SVG for a block's label text.
	RenderText(buf *bytes.Buffer, b Block)
	// RenderPopup writes the SVG for a block's hover popup.
	RenderPopup(buf *bytes.Buffer, b Block)
}

// Block contains all data needed to render a single tower block.
type Block struct {
	ID           string     // Node identifier
	Label        string     // Display text
	X, Y, W, H   float64    // Position and dimensions
	CX, CY       float64    // Center coordinates (for text)
	URL          string     // Optional link target
	Popup        *PopupData // Hover popup content (nil if disabled)
	Brittle      bool       // Whether to apply brittle/warning styling
	VulnSeverity string     // Indicates the maximum vulnerability severity for this package
	License      string     // License name (e.g., "MIT", "GPL-3.0")
	LicenseRisk  string     // License risk classification ("copyleft","weak-copyleft","unknown","")
}

// PopupData holds metadata displayed in hover popups.
type PopupData struct {
	Description  string // Package description
	Stars        int    // GitHub stars (0 if unknown)
	LastCommit   string // Last commit date
	LastRelease  string // Last release date
	Archived     bool   // Repository archived flag
	Brittle      bool   // Package flagged as brittle
	License      string // License name (e.g., "MIT", "GPL-3.0")
	LicenseRisk  string // License risk classification
	VulnSeverity string // Maximum vulnerability severity (e.g., "critical", "high")
}

// Edge contains positioning data for rendering a dependency edge.
type Edge struct {
	FromID, ToID   string  // Connected node IDs
	X1, Y1, X2, Y2 float64 // Line coordinates
}
