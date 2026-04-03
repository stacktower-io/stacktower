package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// =============================================================================
// Color Palette (adaptive for light/dark terminals)
// =============================================================================

var (
	// AdaptiveColor{Light: colorForLightBg, Dark: colorForDarkBg}
	ColorPurple = lipgloss.AdaptiveColor{Light: "93", Dark: "135"}  // Purple - primary actions
	ColorGreen  = lipgloss.AdaptiveColor{Light: "28", Dark: "76"}   // Green - success
	ColorYellow = lipgloss.AdaptiveColor{Light: "136", Dark: "220"} // Yellow - warnings
	ColorRed    = lipgloss.AdaptiveColor{Light: "124", Dark: "167"} // Red - errors
	ColorBlue   = lipgloss.AdaptiveColor{Light: "27", Dark: "75"}   // Blue - links
	ColorWhite  = lipgloss.AdaptiveColor{Light: "235", Dark: "255"} // Text - values (dark on light, white on dark)
	ColorGray   = lipgloss.AdaptiveColor{Light: "242", Dark: "245"} // Gray - secondary text
	ColorDim    = lipgloss.AdaptiveColor{Light: "247", Dark: "240"} // Dim - muted text
)

// =============================================================================
// Public Styles
// =============================================================================

var (
	// StyleTitle for main headings.
	StyleTitle = lipgloss.NewStyle().Bold(true).Foreground(ColorPurple)

	// StyleHighlight for emphasized values.
	StyleHighlight = lipgloss.NewStyle().Foreground(ColorPurple)

	// StyleLink for URLs.
	StyleLink = lipgloss.NewStyle().Foreground(ColorBlue).Underline(true)

	// StyleDim for secondary/muted text.
	StyleDim = lipgloss.NewStyle().Foreground(ColorDim)

	// StyleValue for data values.
	StyleValue = lipgloss.NewStyle().Foreground(ColorWhite)

	// StyleNumber for numeric values.
	StyleNumber = lipgloss.NewStyle().Foreground(ColorPurple)

	// StyleSuccess for success messages.
	StyleSuccess = lipgloss.NewStyle().Foreground(ColorGreen)

	// StyleWarning for warning messages.
	StyleWarning = lipgloss.NewStyle().Foreground(ColorYellow)

	// StyleInfo for informational messages.
	StyleInfo = lipgloss.NewStyle().Foreground(ColorBlue)

	// StyleError for error messages.
	StyleError = lipgloss.NewStyle().Foreground(ColorRed)
)

// =============================================================================
// Internal Styles
// =============================================================================

var (
	StyleIconSuccess = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleIconError   = lipgloss.NewStyle().Foreground(ColorRed)
	StyleIconWarning = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleIconInfo    = lipgloss.NewStyle().Foreground(ColorGray)
	StyleIconSpinner = lipgloss.NewStyle().Foreground(ColorPurple)

	StyleCached   = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleComputed = lipgloss.NewStyle().Foreground(ColorGray)

	StyleCommand  = lipgloss.NewStyle().Foreground(ColorBlue)
	StyleKeyLabel = lipgloss.NewStyle().Foreground(ColorGray).Width(12)
)

// =============================================================================
// Icons
// =============================================================================

const (
	IconSuccess = "✓"
	IconError   = "✗"
	IconWarning = "!"
	IconInfo    = "›"
	IconArrow   = "→"
	IconCached  = "cached"
	IconFresh   = "fresh"
)
