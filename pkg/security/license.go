package security

import (
	"strings"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

// =============================================================================
// License Risk Classification
// =============================================================================

// LicenseRisk classifies how restrictive a license is for downstream consumers.
type LicenseRisk string

const (
	// LicenseRiskPermissive means the license imposes minimal restrictions
	// (e.g., MIT, Apache-2.0, BSD, ISC). No action required.
	LicenseRiskPermissive LicenseRisk = "permissive"

	// LicenseRiskWeakCopyleft means the license requires derivative *library*
	// changes to be shared but allows proprietary linking (e.g., LGPL, MPL).
	LicenseRiskWeakCopyleft LicenseRisk = "weak-copyleft"

	// LicenseRiskCopyleft means the license requires all derivative works to
	// be distributed under the same terms (e.g., GPL, AGPL). This is the
	// most restrictive category and is typically flagged in proprietary projects.
	LicenseRiskCopyleft LicenseRisk = "copyleft"

	// LicenseRiskProprietary means the license is a known commercial/source-available
	// license that restricts usage (e.g., SSPL, BUSL, Commons-Clause, custom commercial).
	// These are NOT open source and typically require paid licenses or have usage restrictions.
	LicenseRiskProprietary LicenseRisk = "proprietary"

	// LicenseRiskUnknown means the license could not be determined. This is
	// flagged because unlicensed code defaults to "all rights reserved".
	LicenseRiskUnknown LicenseRisk = "unknown"
)

// String returns the string representation of the license risk.
func (r LicenseRisk) String() string { return string(r) }

// Weight returns a numeric weight for sorting (higher = more restrictive).
func (r LicenseRisk) Weight() int {
	switch r {
	case LicenseRiskProprietary:
		return 4 // most restrictive - commercial restrictions
	case LicenseRiskCopyleft:
		return 3
	case LicenseRiskWeakCopyleft:
		return 2
	case LicenseRiskUnknown:
		return 1
	default:
		return 0
	}
}

// IsFlagged returns true if this risk level should be visually indicated.
// Permissive licenses are not flagged.
func (r LicenseRisk) IsFlagged() bool {
	return r != LicenseRiskPermissive && r != ""
}

// =============================================================================
// License Risk Colors — used by renderers for flag icon indicators
// =============================================================================

// IconColor returns the fill colour for the license-risk flag icon.
// Only called when IsFlagged() is true.
func (r LicenseRisk) IconColor() string {
	switch r {
	case LicenseRiskProprietary:
		return "#dc2626" // red-600 — proprietary/commercial
	case LicenseRiskCopyleft:
		return "#9333ea" // purple-600 — strong copyleft
	case LicenseRiskWeakCopyleft:
		return "#a855f7" // purple-500 — weak copyleft
	case LicenseRiskUnknown:
		return "#6b7280" // gray-500 — unknown
	default:
		return ""
	}
}

// LicenseRiskFromString converts a string to a LicenseRisk value.
// Returns empty string for unrecognised values.
func LicenseRiskFromString(s string) LicenseRisk {
	switch LicenseRisk(s) {
	case LicenseRiskPermissive, LicenseRiskWeakCopyleft, LicenseRiskCopyleft, LicenseRiskProprietary, LicenseRiskUnknown:
		return LicenseRisk(s)
	default:
		return ""
	}
}

// =============================================================================
// Metadata Keys — for DAG node annotation
// =============================================================================

// MetaLicense is the dag.Node.Meta key for the license identifier/SPDX.
// This is typically a short identifier like "MIT", "Apache-2.0", etc.
// Populated by registry fetchers during dependency resolution.
const MetaLicense = "license"

// MetaLicenseText is the dag.Node.Meta key for the full raw license text.
// Used when the license is a custom/non-standard license (e.g., full legal text).
// Can be fed to an LLM for downstream analysis.
const MetaLicenseText = "license_text"

// MetaLicenseRisk is the dag.Node.Meta key used to store the license risk
// classification for a given package. The value is a LicenseRisk string.
const MetaLicenseRisk = "license_risk"

// =============================================================================
// License Classification
// =============================================================================

// ClassifyLicense determines the risk category for a license string.
// It handles SPDX identifiers, common names, and compound expressions
// (e.g., "MIT OR Apache-2.0"). For compound OR expressions, the least
// restrictive alternative is used (the user can choose).
func ClassifyLicense(license string) LicenseRisk {
	license = strings.TrimSpace(license)
	if license == "" {
		return LicenseRiskUnknown
	}

	upper := strings.ToUpper(license)

	// Only process OR/AND as compound SPDX expressions for short license strings.
	// Long strings (>100 chars) are likely full license text where "or"/"and" are
	// just regular words (e.g., "you may not redistribute or sublicense...").
	// SPDX compound expressions are always short like "MIT OR Apache-2.0".
	if len(license) <= 100 {
		// Handle compound OR expressions: "MIT OR Apache-2.0"
		// With OR, the least restrictive option applies (user chooses)
		if strings.Contains(upper, " OR ") {
			parts := splitIgnoreCase(license, " OR ")
			best := LicenseRiskCopyleft // start with most restrictive
			for _, part := range parts {
				risk := classifySingle(strings.TrimSpace(part))
				if risk.Weight() < best.Weight() {
					best = risk
				}
			}
			return best
		}

		// Handle compound AND expressions: "MIT AND BSD-3-Clause"
		// With AND, the most restrictive option applies (must comply with all)
		if strings.Contains(upper, " AND ") {
			parts := splitIgnoreCase(license, " AND ")
			worst := LicenseRiskPermissive
			for _, part := range parts {
				risk := classifySingle(strings.TrimSpace(part))
				if risk.Weight() > worst.Weight() {
					worst = risk
				}
			}
			return worst
		}
	}

	return classifySingle(license)
}

// splitIgnoreCase splits s by sep case-insensitively.
func splitIgnoreCase(s, sep string) []string {
	upper := strings.ToUpper(s)
	sepUpper := strings.ToUpper(sep)
	indices := []int{0}
	offset := 0
	for {
		idx := strings.Index(upper[offset:], sepUpper)
		if idx < 0 {
			break
		}
		pos := offset + idx
		indices = append(indices, pos, pos+len(sep))
		offset = pos + len(sep)
	}
	indices = append(indices, len(s))

	var parts []string
	for i := 0; i < len(indices)-1; i += 2 {
		part := s[indices[i]:indices[i+1]]
		parts = append(parts, part)
	}
	return parts
}

// classifySingle classifies a single license identifier.
func classifySingle(license string) LicenseRisk {
	normalized := strings.ToLower(strings.TrimSpace(license))

	// Remove common suffixes and noise
	normalized = strings.TrimSuffix(normalized, " license")
	normalized = strings.TrimSuffix(normalized, " licence")
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return LicenseRiskUnknown
	}

	// Check proprietary first — includes known restrictive licenses and
	// heuristic detection of custom commercial licenses
	if isProprietary(license, normalized) {
		return LicenseRiskProprietary
	}

	// Check copyleft (strong copyleft)
	if isCopyleft(normalized) {
		return LicenseRiskCopyleft
	}

	// Check weak copyleft
	if isWeakCopyleft(normalized) {
		return LicenseRiskWeakCopyleft
	}

	// Check permissive
	if isPermissive(normalized) {
		return LicenseRiskPermissive
	}

	// Fallback: if we have license text (not just an identifier), use heuristics
	// to detect if it's likely permissive or copyleft
	if len(license) > 50 {
		if fallback := detectLicenseFromText(license); fallback != LicenseRiskUnknown {
			return fallback
		}
	}

	// If we don't recognise the license, mark as unknown
	return LicenseRiskUnknown
}

// detectLicenseFromText is a fallback for unrecognized license text.
// We intentionally DON'T try to guess the license type from text heuristics
// because it's too error-prone (e.g., "without warranty" appears in both
// permissive AND proprietary licenses). Instead, we mark it as unknown
// so users can review the actual license text.
func detectLicenseFromText(text string) LicenseRisk {
	// Don't guess - if we couldn't match a known SPDX identifier,
	// it needs human review
	return LicenseRiskUnknown
}

// isCopyleft returns true for strong copyleft licenses.
func isCopyleft(license string) bool {
	copyleftPrefixes := []string{
		"gpl", "gnu general public",
		"agpl", "gnu affero",
		"sspl", "server side public",
		"eupl",
		"osl", "open software",
		"rpsl",
		"qpl",
		"sleepycat",
	}
	for _, prefix := range copyleftPrefixes {
		if strings.HasPrefix(license, prefix) {
			// Exclude LGPL which is weak copyleft
			if strings.HasPrefix(license, "lgpl") || strings.HasPrefix(license, "lesser") {
				return false
			}
			return true
		}
	}
	return false
}

// isWeakCopyleft returns true for weak copyleft licenses.
func isWeakCopyleft(license string) bool {
	weakCopyleftPrefixes := []string{
		"lgpl", "gnu lesser",
		"gnu library",
		"mpl", "mozilla public",
		"cddl", "common development",
		"epl", "eclipse public",
		"cecill", "cecill-c",
		"artistic",
		"ofl",
		"eupl",
	}
	for _, prefix := range weakCopyleftPrefixes {
		if strings.HasPrefix(license, prefix) {
			return true
		}
	}

	// Check for LGPL anywhere in the string (handles variations)
	if strings.Contains(license, "(lgpl)") || strings.Contains(license, "lgpl-") {
		return true
	}

	// Handle specific SPDX identifiers that may not match prefixes
	weakCopyleftExact := map[string]bool{
		"mpl-2.0":    true,
		"lgpl-2.0":   true,
		"lgpl-2.1":   true,
		"lgpl-3.0":   true,
		"epl-1.0":    true,
		"epl-2.0":    true,
		"cddl-1.0":   true,
		"cddl-1.1":   true,
		"cecill-2.1": true,
		"ofl-1.1":    true,
	}
	return weakCopyleftExact[license]
}

// isPermissive returns true for common permissive licenses.
func isPermissive(license string) bool {
	permissivePrefixes := []string{
		"mit", "isc", "bsd", "apache", "unlicense", "wtfpl",
		"cc0", "public domain", "0bsd", "zlib", "boost",
		"bsl", "python", "psf", "unicode",
	}
	for _, prefix := range permissivePrefixes {
		if strings.HasPrefix(license, prefix) {
			return true
		}
	}

	// Handle specific SPDX identifiers
	permissiveExact := map[string]bool{
		"mit":              true,
		"isc":              true,
		"bsd-2-clause":     true,
		"bsd-3-clause":     true,
		"apache-2.0":       true,
		"unlicense":        true,
		"cc0-1.0":          true,
		"0bsd":             true,
		"zlib":             true,
		"bsl-1.0":          true,
		"wtfpl":            true,
		"postgresql":       true,
		"ncsa":             true,
		"x11":              true,
		"unicode-dfs-2016": true,
		"python-2.0":       true,
		"psf-2.0":          true,
	}
	return permissiveExact[license]
}

// isProprietary returns true for commercial, source-available, or restrictive licenses.
// It checks both known SPDX identifiers AND applies heuristics for custom license text.
// The original (non-normalized) license is passed for length-based heuristics.
func isProprietary(original, normalized string) bool {
	// Known proprietary/source-available SPDX identifiers
	proprietaryPrefixes := []string{
		"sspl", "server side public",
		"busl", "business source",
		"elastic", "elasticv2",
		"confluent community",
		"polyform",
		"commons clause", "commons-clause",
		"mariadb business",
		"timescale",
		"cockroach",
	}
	for _, prefix := range proprietaryPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	proprietaryExact := map[string]bool{
		"sspl-1.0":        true,
		"busl-1.1":        true,
		"elastic-2.0":     true,
		"commons-clause":  true,
		"polyform-nc-1.0": true,
		"polyform-sb-1.0": true,
	}
	if proprietaryExact[normalized] {
		return true
	}

	// Heuristic: very long "license" strings are likely full license text, not SPDX IDs.
	// SPDX identifiers are typically < 50 chars. Custom license text is much longer.
	if len(original) > 200 {
		lowerOriginal := strings.ToLower(original)

		// First, check if this is clearly an open source license - if so, don't flag as proprietary
		// even if it mentions "proprietary" in some context (e.g., "linking with proprietary software")
		openSourceIndicators := []string{
			"gnu general public license",
			"gnu lesser general public license",
			"permission is hereby granted, free of charge",
			"permission is hereby granted",
			"mozilla public license",
			"apache license",
			"mit license",
			"bsd license",
			"free software",
			"open source",
			"redistributions of source code",
		}
		for _, indicator := range openSourceIndicators {
			if strings.Contains(lowerOriginal, indicator) {
				return false // Clearly open source, not proprietary
			}
		}

		// Check for commercial/proprietary indicators in the text
		proprietaryIndicators := []string{
			"subscription fee",
			"subscription term",
			"proprietary software", // Changed from just "proprietary" to be more specific
			"this is proprietary",
			"commercial license",
			"all rights reserved",
			"may not redistribute",
			"may not copy",
			"may not modify",
			"may not sublicense",
			"not open source",
			"source-available",
			"paid license",
			"license fee",
			"shall not",
			"you may not",
		}
		for _, indicator := range proprietaryIndicators {
			if strings.Contains(lowerOriginal, indicator) {
				return true
			}
		}
	}

	return false
}

// =============================================================================
// License Report
// =============================================================================

// LicenseInfo contains detailed license information for a single package.
// This structure captures both the identifier and full text for LLM analysis.
type LicenseInfo struct {
	// Package is the package/dependency identifier (e.g., "lodash", "requests").
	Package string `json:"package" bson:"package"`

	// License is the license identifier or title (e.g., "MIT", "Apache-2.0", or custom name).
	License string `json:"license" bson:"license"`

	// LicenseText is the full raw license text, if available.
	// Populated for custom/proprietary licenses to enable LLM analysis.
	// May be empty for standard SPDX licenses where the text is well-known.
	LicenseText string `json:"license_text,omitempty" bson:"licenseText,omitempty"`

	// Risk is the classified risk level for this license.
	Risk LicenseRisk `json:"risk" bson:"risk"`
}

// LicenseReport summarises license compliance across all dependencies.
type LicenseReport struct {
	// Licenses maps license names to the list of packages using that license.
	// Example: "MIT": ["express", "lodash"]
	Licenses map[string][]string `json:"licenses" bson:"licenses"`

	// Details contains full license information for each dependency.
	// Keyed by package ID for easy lookup. Includes license text when available.
	Details map[string]*LicenseInfo `json:"details,omitempty" bson:"details,omitempty"`

	// Copyleft lists packages with copyleft licenses that may require attention.
	Copyleft []string `json:"copyleft" bson:"copyleft"`

	// WeakCopyleft lists packages with weak copyleft licenses (LGPL, MPL, etc.).
	// BSON tag uses camelCase to match default Go MongoDB driver behavior for existing data.
	WeakCopyleft []string `json:"weak_copyleft" bson:"weakCopyleft"`

	// Proprietary lists packages with commercial/source-available licenses
	// (SSPL, BUSL, Commons-Clause, or detected custom commercial licenses).
	Proprietary []string `json:"proprietary" bson:"proprietary"`

	// Unknown lists packages where the license could not be determined.
	Unknown []string `json:"unknown" bson:"unknown"`

	// Compliant is true when no copyleft, proprietary, or unknown licenses are present.
	// Note: weak-copyleft licenses do NOT break compliance by default since
	// they typically allow linking from proprietary code.
	Compliant bool `json:"compliant" bson:"compliant"`

	// TotalDeps is the number of dependencies that were analysed.
	TotalDeps int `json:"total_deps" bson:"totalDeps"`
}

// =============================================================================
// Graph Integration
// =============================================================================

// AnalyzeLicenses scans all nodes in a DAG for license metadata and produces
// a LicenseReport. It also annotates each node with a MetaLicenseRisk value.
//
// License data is read from the node's "license" metadata key (populated by
// registry fetchers during dependency resolution).
//
// This function is safe to call on a nil DAG (returns an empty report).
func AnalyzeLicenses(g *dag.DAG) *LicenseReport {
	if g == nil {
		return &LicenseReport{
			Licenses:  make(map[string][]string),
			Details:   make(map[string]*LicenseInfo),
			Compliant: true,
		}
	}

	report := &LicenseReport{
		Licenses: make(map[string][]string),
		Details:  make(map[string]*LicenseInfo),
	}

	for _, n := range g.Nodes() {
		// Skip synthetic nodes, project root markers, and root-level nodes (Row 0).
		// Root nodes are the packages being analyzed (user's own code), not dependencies.
		if n.IsSynthetic() || n.ID == "__project__" || n.Row == 0 {
			continue
		}

		report.TotalDeps++

		// Read license identifier and text from metadata
		license := ""
		licenseText := ""
		if n.Meta != nil {
			license, _ = n.Meta[MetaLicense].(string)
			licenseText, _ = n.Meta[MetaLicenseText].(string)
		}

		// First try to classify by the short license identifier
		risk := ClassifyLicense(license)

		// If unknown and we have full license text, try classifying based on that
		// This catches proprietary licenses where the identifier is unrecognized
		// but the full text contains commercial/restrictive indicators
		if risk == LicenseRiskUnknown && licenseText != "" {
			textRisk := ClassifyLicense(licenseText)
			if textRisk != LicenseRiskUnknown {
				risk = textRisk
			}
		}

		// Annotate the node
		if n.Meta == nil {
			n.Meta = dag.Metadata{}
		}
		n.Meta[MetaLicenseRisk] = string(risk)

		// Create detailed license info entry
		// Store license text for non-permissive licenses (useful for LLM analysis)
		info := &LicenseInfo{
			Package: n.ID,
			License: license,
			Risk:    risk,
		}
		// Include text for proprietary/unknown licenses, or if text differs significantly
		// from standard licenses (indicating custom terms)
		if risk == LicenseRiskProprietary || risk == LicenseRiskUnknown || len(licenseText) > 200 {
			info.LicenseText = licenseText
		}
		report.Details[n.ID] = info

		// Populate license grouping
		if license != "" {
			report.Licenses[license] = append(report.Licenses[license], n.ID)
		}

		switch risk {
		case LicenseRiskProprietary:
			report.Proprietary = append(report.Proprietary, n.ID)
		case LicenseRiskCopyleft:
			report.Copyleft = append(report.Copyleft, n.ID)
		case LicenseRiskWeakCopyleft:
			report.WeakCopyleft = append(report.WeakCopyleft, n.ID)
		case LicenseRiskUnknown:
			report.Unknown = append(report.Unknown, n.ID)
		}
	}

	// Compliant = no copyleft, proprietary, or unknown licenses
	report.Compliant = len(report.Copyleft) == 0 && len(report.Proprietary) == 0 && len(report.Unknown) == 0

	return report
}

// StripLicenseData removes license risk metadata from all nodes in a DAG.
// This is used when ShowLicenses is false — the renderers will not see license data.
func StripLicenseData(g *dag.DAG) {
	for _, n := range g.Nodes() {
		if n.Meta != nil {
			delete(n.Meta, MetaLicenseRisk)
		}
	}
}
