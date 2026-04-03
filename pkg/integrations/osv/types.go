package osv

import "time"

// =============================================================================
// Request Types
// =============================================================================

// BatchRequest is the request body for the /v1/querybatch endpoint.
type BatchRequest struct {
	Queries []Query `json:"queries"`
}

// Query represents a single vulnerability lookup.
type Query struct {
	Package PackageQuery `json:"package"`
	Version string       `json:"version,omitempty"`
}

// PackageQuery identifies a package in a specific ecosystem.
type PackageQuery struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// =============================================================================
// Response Types
// =============================================================================

// BatchResponse is the response from the /v1/querybatch endpoint.
type BatchResponse struct {
	Results []QueryResult `json:"results"`
}

// QueryResult contains vulnerabilities found for a single query.
type QueryResult struct {
	Vulns []Vulnerability `json:"vulns,omitempty"`
}

// Vulnerability represents a single OSV vulnerability record.
type Vulnerability struct {
	ID       string    `json:"id"`
	Summary  string    `json:"summary"`
	Details  string    `json:"details,omitempty"`
	Aliases  []string  `json:"aliases,omitempty"`
	Modified time.Time `json:"modified"`

	// Severity from the database_specific or ecosystem_specific fields.
	Severity []SeverityEntry `json:"severity,omitempty"`

	// Affected packages and version ranges.
	Affected []Affected `json:"affected,omitempty"`

	// References to advisories, patches, etc.
	References []Reference `json:"references,omitempty"`
}

// SeverityEntry contains a CVSS score or severity string.
type SeverityEntry struct {
	Type  string `json:"type"`  // e.g., "CVSS_V3"
	Score string `json:"score"` // e.g., "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"
}

// Affected describes the affected package versions.
type Affected struct {
	Package  AffectedPackage `json:"package"`
	Ranges   []Range         `json:"ranges,omitempty"`
	Versions []string        `json:"versions,omitempty"`
}

// AffectedPackage identifies an affected package.
type AffectedPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// Range describes a version range for the vulnerability.
type Range struct {
	Type   string  `json:"type"` // "SEMVER", "GIT", "ECOSYSTEM"
	Events []Event `json:"events,omitempty"`
}

// Event marks a point in the version history where the vulnerability
// was introduced or fixed.
type Event struct {
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}

// Reference is a link to an advisory, patch, or other resource.
type Reference struct {
	Type string `json:"type"` // "ADVISORY", "FIX", "WEB", etc.
	URL  string `json:"url"`
}
