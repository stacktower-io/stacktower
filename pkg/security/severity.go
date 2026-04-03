package security

// Severity represents the severity level of a vulnerability finding.
type Severity string

const (
	// SeverityCritical indicates a critical vulnerability requiring immediate attention.
	SeverityCritical Severity = "critical"

	// SeverityHigh indicates a high-severity vulnerability.
	SeverityHigh Severity = "high"

	// SeverityMedium indicates a medium-severity vulnerability.
	SeverityMedium Severity = "medium"

	// SeverityLow indicates a low-severity vulnerability.
	SeverityLow Severity = "low"

	// SeverityUnknown is used when severity cannot be determined.
	SeverityUnknown Severity = "unknown"
)

// String returns the string representation of the severity.
func (s Severity) String() string { return string(s) }

// IsHighOrCritical returns true for critical or high severity findings.
func (s Severity) IsHighOrCritical() bool {
	return s == SeverityCritical || s == SeverityHigh
}

// Weight returns a numeric weight for sorting (higher = more severe).
func (s Severity) Weight() int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// =============================================================================
// Color Mapping — used by renderers for vulnerability visualization
// =============================================================================

// Color returns the hex fill color for this severity level.
// Healthy/unaffected nodes should keep their default style (grey or white);
// this method is only called when a vulnerability is present.
func (s Severity) Color() string {
	switch s {
	case SeverityCritical:
		return "#dc2626" // red-600
	case SeverityHigh:
		return "#ea580c" // orange-600
	case SeverityMedium:
		return "#d97706" // amber-600
	case SeverityLow:
		return "#eab308" // yellow-500
	default:
		return "#fbbf24" // amber-400 (unknown but flagged)
	}
}

// TextColor returns a contrasting text color for this severity's background.
func (s Severity) TextColor() string {
	switch s {
	case SeverityCritical, SeverityHigh:
		return "#ffffff"
	default:
		return "#333333"
	}
}

// DOTColor returns the Graphviz fill color name or hex for DOT rendering.
func (s Severity) DOTColor() string { return s.Color() }

// SeverityFromString converts a string to a Severity value.
// Returns SeverityUnknown for unrecognised strings.
func SeverityFromString(s string) Severity {
	switch Severity(s) {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
		return Severity(s)
	default:
		return SeverityUnknown
	}
}
