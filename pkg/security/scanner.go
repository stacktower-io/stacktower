package security

import (
	"context"
	"time"

	"github.com/matzehuels/stacktower/pkg/core/dag"
	"github.com/matzehuels/stacktower/pkg/graph"
)

// Scanner analyzes dependencies for known vulnerabilities.
//
// Implementations query vulnerability databases (e.g., OSV.dev) and return
// structured reports. Scanners should support batch queries for efficiency.
//
// Scanner implementations must be safe for concurrent use.
type Scanner interface {
	// Scan analyzes the given dependencies and returns a vulnerability report.
	// An empty dependency list returns an empty report (not an error).
	// Network errors or API failures are returned as errors.
	Scan(ctx context.Context, deps []Dependency) (*Report, error)
}

// Dependency identifies a package to scan for vulnerabilities.
type Dependency struct {
	// Name is the package name as it appears in the registry.
	Name string `json:"name"`

	// Version is the specific version to check. Empty means "latest".
	Version string `json:"version"`

	// Ecosystem identifies the package ecosystem for vulnerability matching.
	// Values follow the OSV ecosystem convention: "npm", "PyPI", "Go",
	// "crates.io", "Maven", "RubyGems", "Packagist".
	Ecosystem string `json:"ecosystem"`
}

// Report contains the results of a vulnerability scan.
type Report struct {
	// Findings is the list of individual vulnerability findings.
	Findings []Finding `json:"findings"`

	// SeveritySummary counts findings by severity level.
	SeveritySummary map[Severity]int `json:"severity_summary"`

	// TotalDeps is the number of dependencies that were scanned.
	TotalDeps int `json:"total_deps"`

	// VulnerableDeps is the number of dependencies with at least one finding.
	VulnerableDeps int `json:"vulnerable_deps"`

	// ScannedAt is the timestamp when the scan was performed.
	ScannedAt time.Time `json:"scanned_at"`
}

// Finding represents a single vulnerability associated with a dependency.
type Finding struct {
	// ID is the primary vulnerability identifier (e.g., "GHSA-xxxx" or "PYSEC-2024-123").
	ID string `json:"id"`

	// Aliases are alternative identifiers (e.g., CVE numbers).
	Aliases []string `json:"aliases,omitempty"`

	// Package is the affected package name.
	Package string `json:"package"`

	// Version is the affected version.
	Version string `json:"version"`

	// Ecosystem is the package ecosystem.
	Ecosystem string `json:"ecosystem"`

	// Summary is a short human-readable description.
	Summary string `json:"summary"`

	// Details is the full vulnerability description (may be empty).
	Details string `json:"details,omitempty"`

	// Severity is the assessed severity level.
	Severity Severity `json:"severity"`

	// FixVersions lists versions that fix this vulnerability (may be empty).
	FixVersions []string `json:"fix_versions,omitempty"`

	// References contains URLs to advisories, patches, etc.
	References []string `json:"references,omitempty"`
}

// =============================================================================
// Metadata Key — for DAG node annotation
// =============================================================================

// MetaVulnSeverity is the dag.Node.Meta key used to store the maximum
// vulnerability severity for a given package. The value is a Severity string.
const MetaVulnSeverity = "vuln_severity"

// =============================================================================
// Graph Integration
// =============================================================================

// PackageSeverities returns the maximum severity per package from a report.
// The map keys are package names matching node IDs in the dependency graph.
func PackageSeverities(r *Report) map[string]Severity {
	if r == nil {
		return nil
	}
	result := make(map[string]Severity)
	for _, f := range r.Findings {
		if existing, ok := result[f.Package]; !ok || f.Severity.Weight() > existing.Weight() {
			result[f.Package] = f.Severity
		}
	}
	return result
}

// AnnotateGraph writes vulnerability severity data into DAG node metadata.
// After this call, affected nodes will have a MetaVulnSeverity key in their
// Meta map. Nodes without findings are left untouched. Safe to call with a
// nil report (no-op).
func AnnotateGraph(g *dag.DAG, report *Report) {
	severities := PackageSeverities(report)
	if len(severities) == 0 {
		return
	}
	for _, n := range g.Nodes() {
		if sev, ok := severities[n.ID]; ok {
			if n.Meta == nil {
				n.Meta = dag.Metadata{}
			}
			n.Meta[MetaVulnSeverity] = string(sev)
		}
	}
}

// EcosystemFromLanguage maps stacktower language identifiers to OSV ecosystem names.
func EcosystemFromLanguage(language string) string {
	switch language {
	case "python":
		return "PyPI"
	case "javascript":
		return "npm"
	case "go", "golang":
		return "Go"
	case "rust":
		return "crates.io"
	case "java":
		return "Maven"
	case "ruby":
		return "RubyGems"
	case "php":
		return "Packagist"
	default:
		return language
	}
}

// rootPackageID returns the ID of the root package in a serialized graph —
// the single non-synthetic node with no incoming edges.
//
// We exclude the root from vulnerability scanning because it represents the
// package or repository being investigated: its own vulnerabilities are not
// something the investigation can act on. Only its dependencies matter.
func rootPackageID(g graph.Graph) string {
	targets := make(map[string]struct{}, len(g.Edges))
	for _, e := range g.Edges {
		targets[e.To] = struct{}{}
	}
	for _, n := range g.Nodes {
		if n.Kind != "" || n.ID == "__project__" {
			continue
		}
		if _, isTarget := targets[n.ID]; !isTarget {
			return n.ID
		}
	}
	return ""
}

// DependenciesFromGraph extracts scannable dependencies from a serialized graph.
// It reads package name from node IDs and version from node metadata.
// The root package (the investigated package itself) is excluded — its own
// vulnerabilities are out of scope for the investigation.
func DependenciesFromGraph(g graph.Graph, language string) []Dependency {
	ecosystem := EcosystemFromLanguage(language)
	rootID := rootPackageID(g)
	deps := make([]Dependency, 0, len(g.Nodes))

	for _, node := range g.Nodes {
		// Skip synthetic nodes, the project root sentinel, and the root package.
		if node.Kind != "" || node.ID == "__project__" || node.ID == rootID {
			continue
		}

		version := ""
		if node.Meta != nil {
			if v, ok := node.Meta["version"].(string); ok {
				version = v
			}
		}

		deps = append(deps, Dependency{
			Name:      node.ID,
			Version:   version,
			Ecosystem: ecosystem,
		})
	}

	return deps
}

// DependenciesFromDAG extracts scannable dependencies from an in-memory DAG.
// This is the pipeline-friendly counterpart to [DependenciesFromGraph].
// The root package (InDegree == 0, non-synthetic) is excluded for the same
// reason as DependenciesFromGraph.
func DependenciesFromDAG(g *dag.DAG, language string) []Dependency {
	ecosystem := EcosystemFromLanguage(language)
	nodes := g.Nodes()
	deps := make([]Dependency, 0, len(nodes))

	for _, n := range nodes {
		// Skip synthetic, project root sentinel, and the root package itself.
		if n.IsSynthetic() || n.ID == "__project__" {
			continue
		}
		if g.InDegree(n.ID) == 0 {
			// This is the root of the dependency tree — we're investigating
			// *its* dependencies, not its own vulnerability surface.
			continue
		}

		version := ""
		if n.Meta != nil {
			if v, ok := n.Meta["version"].(string); ok {
				version = v
			}
		}

		deps = append(deps, Dependency{
			Name:      n.ID,
			Version:   version,
			Ecosystem: ecosystem,
		})
	}

	return deps
}

// StripVulnData removes vulnerability severity metadata from all nodes in a DAG.
// This is used when ShowVulns is false — the renderers will not see vuln data.
func StripVulnData(g *dag.DAG) {
	for _, n := range g.Nodes() {
		if n.Meta != nil {
			delete(n.Meta, MetaVulnSeverity)
		}
	}
}

// =============================================================================
// Report Helpers
// =============================================================================

// NewReport creates an empty report with the current timestamp.
func NewReport(totalDeps int) *Report {
	return &Report{
		Findings:        make([]Finding, 0),
		SeveritySummary: make(map[Severity]int),
		TotalDeps:       totalDeps,
		ScannedAt:       time.Now(),
	}
}

// AddFinding adds a finding to the report and updates summary counts.
func (r *Report) AddFinding(f Finding) {
	r.Findings = append(r.Findings, f)
	r.SeveritySummary[f.Severity]++
}

// HasCritical returns true if the report contains critical severity findings.
func (r *Report) HasCritical() bool {
	return r.SeveritySummary[SeverityCritical] > 0
}

// HasHighOrCritical returns true if the report contains high or critical findings.
func (r *Report) HasHighOrCritical() bool {
	return r.SeveritySummary[SeverityCritical] > 0 || r.SeveritySummary[SeverityHigh] > 0
}

// MaxSeverity returns the highest severity level found in the report.
func (r *Report) MaxSeverity() Severity {
	if r.SeveritySummary[SeverityCritical] > 0 {
		return SeverityCritical
	}
	if r.SeveritySummary[SeverityHigh] > 0 {
		return SeverityHigh
	}
	if r.SeveritySummary[SeverityMedium] > 0 {
		return SeverityMedium
	}
	if r.SeveritySummary[SeverityLow] > 0 {
		return SeverityLow
	}
	return SeverityUnknown
}
