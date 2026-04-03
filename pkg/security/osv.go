package security

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/matzehuels/stacktower/pkg/integrations/osv"
	"github.com/matzehuels/stacktower/pkg/observability"
)

const enrichConcurrency = 10
const enrichRequestTimeout = 10 * time.Second

// OSVScanner implements [Scanner] using the OSV.dev vulnerability database.
//
// OSVScanner uses batch queries for efficiency — a single API call can check
// hundreds of packages. Results are mapped to the generic [Finding] type.
//
// OSVScanner is safe for concurrent use.
type OSVScanner struct {
	client *osv.Client
}

// NewOSVScanner creates a scanner backed by OSV.dev.
// If client is nil, a default client is created.
func NewOSVScanner(client *osv.Client) *OSVScanner {
	if client == nil {
		client = osv.NewClient(nil, 0)
	}
	return &OSVScanner{client: client}
}

// Scan queries OSV.dev for vulnerabilities in the given dependencies.
//
// Dependencies are batched into a single API call. The returned report
// contains one [Finding] per (package, vulnerability) pair.
//
// Returns an error only for network/API failures. An empty dependency
// list returns an empty report.
func (s *OSVScanner) Scan(ctx context.Context, deps []Dependency) (*Report, error) {
	report := NewReport(len(deps))

	if len(deps) == 0 {
		return report, nil
	}

	ecosystem := ""
	if len(deps) > 0 {
		ecosystem = deps[0].Ecosystem
	}
	observability.Security().OnScanStart(ctx, ecosystem, len(deps))
	start := time.Now()

	// Build OSV queries from dependencies
	queries := make([]osv.Query, len(deps))
	for i, dep := range deps {
		queries[i] = osv.Query{
			Package: osv.PackageQuery{
				Name:      dep.Name,
				Ecosystem: dep.Ecosystem,
			},
			Version: dep.Version,
		}
	}

	results, err := s.client.QueryBatch(ctx, queries, false)
	if err != nil {
		scanErr := fmt.Errorf("osv scan: %w", err)
		observability.Security().OnScanComplete(ctx, ecosystem, 0, time.Since(start), scanErr)
		return nil, scanErr
	}

	// Collect unique vuln IDs that need enrichment before we can build findings.
	type vulnRef struct {
		depIdx int
		vuln   osv.Vulnerability
	}

	var toEnrich []string // unique IDs needing a GetVulnerability call
	seen := make(map[string]bool)
	var refs []vulnRef

	for i, result := range results {
		if i >= len(deps) {
			break
		}
		for _, vuln := range result.Vulns {
			refs = append(refs, vulnRef{depIdx: i, vuln: vuln})
			if needsVulnerabilityEnrichment(vuln) && vuln.ID != "" && !seen[vuln.ID] {
				seen[vuln.ID] = true
				toEnrich = append(toEnrich, vuln.ID)
			}
		}
	}

	// Fetch full details concurrently with a bounded worker pool.
	enriched := make(map[string]*osv.Vulnerability, len(toEnrich))
	if len(toEnrich) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, enrichConcurrency)

		for _, id := range toEnrich {
			wg.Add(1)
			go func(vulnID string) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()

				enrichCtx, cancel := context.WithTimeout(ctx, enrichRequestTimeout)
				defer cancel()
				detail, err := s.client.GetVulnerability(enrichCtx, vulnID, false)
				mu.Lock()
				if err == nil && detail != nil {
					enriched[vulnID] = detail
				} else {
					enriched[vulnID] = nil
				}
				mu.Unlock()
			}(id)
		}
		wg.Wait()
	}

	// Build findings from the collected refs + enriched data.
	vulnerableSet := make(map[string]bool)
	for _, ref := range refs {
		dep := deps[ref.depIdx]
		v := ref.vuln
		if detail, ok := enriched[v.ID]; ok && detail != nil {
			v = mergeVulnerability(v, *detail)
		}
		report.AddFinding(vulnToFinding(v, dep))
		vulnerableSet[dep.Name] = true
	}

	report.VulnerableDeps = len(vulnerableSet)
	observability.Security().OnScanComplete(ctx, ecosystem, len(report.Findings), time.Since(start), nil)
	return report, nil
}

func needsVulnerabilityEnrichment(v osv.Vulnerability) bool {
	return v.Summary == "" ||
		v.Details == "" ||
		len(v.References) == 0 ||
		len(v.Affected) == 0 ||
		len(v.Severity) == 0
}

func mergeVulnerability(base, full osv.Vulnerability) osv.Vulnerability {
	merged := base
	if merged.Summary == "" {
		merged.Summary = full.Summary
	}
	if merged.Details == "" {
		merged.Details = full.Details
	}
	if len(merged.Aliases) == 0 {
		merged.Aliases = full.Aliases
	}
	if len(merged.Severity) == 0 {
		merged.Severity = full.Severity
	}
	if len(merged.Affected) == 0 {
		merged.Affected = full.Affected
	}
	if len(merged.References) == 0 {
		merged.References = full.References
	}
	return merged
}

// vulnToFinding converts an OSV vulnerability to a generic Finding.
func vulnToFinding(v osv.Vulnerability, dep Dependency) Finding {
	f := Finding{
		ID:        v.ID,
		Aliases:   v.Aliases,
		Package:   dep.Name,
		Version:   dep.Version,
		Ecosystem: dep.Ecosystem,
		Summary:   v.Summary,
		Details:   v.Details,
		Severity:  extractSeverity(v),
	}

	// Extract fix versions from affected ranges
	for _, affected := range v.Affected {
		for _, r := range affected.Ranges {
			for _, event := range r.Events {
				if event.Fixed != "" {
					f.FixVersions = append(f.FixVersions, event.Fixed)
				}
			}
		}
	}

	// Extract reference URLs
	for _, ref := range v.References {
		if ref.URL != "" {
			f.References = append(f.References, ref.URL)
		}
	}

	return f
}

// extractSeverity determines the severity from OSV vulnerability data.
// It checks CVSS scores first, then falls back to alias-based heuristics.
func extractSeverity(v osv.Vulnerability) Severity {
	// Try CVSS v3 score first
	for _, s := range v.Severity {
		if s.Type == "CVSS_V3" && s.Score != "" {
			return severityFromCVSS(s.Score)
		}
	}

	// Fall back to alias-based heuristics
	// GHSA advisories in their ID often indicate severity
	id := strings.ToUpper(v.ID)
	if strings.HasPrefix(id, "GHSA-") {
		// GitHub Security Advisories don't encode severity in the ID,
		// but their presence indicates a known issue
		return SeverityMedium
	}

	return SeverityUnknown
}

// severityFromCVSS extracts severity from a CVSS v3 vector string.
// Format: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" (score at end sometimes)
// CVSS v3 severity ranges: 0.0 = None, 0.1-3.9 = Low, 4.0-6.9 = Medium,
// 7.0-8.9 = High, 9.0-10.0 = Critical.
func severityFromCVSS(vector string) Severity {
	// Parse CVSS vector to estimate severity from impact metrics
	v := strings.ToUpper(vector)

	// Count high-impact metrics
	highImpacts := 0
	if strings.Contains(v, "/C:H") {
		highImpacts++
	}
	if strings.Contains(v, "/I:H") {
		highImpacts++
	}
	if strings.Contains(v, "/A:H") {
		highImpacts++
	}

	// Check access vector and complexity
	networkAccess := strings.Contains(v, "/AV:N")
	lowComplexity := strings.Contains(v, "/AC:L")
	noPriv := strings.Contains(v, "/PR:N")

	switch {
	case highImpacts >= 2 && networkAccess && lowComplexity && noPriv:
		return SeverityCritical
	case highImpacts >= 2 && networkAccess:
		return SeverityHigh
	case highImpacts >= 1:
		return SeverityMedium
	default:
		return SeverityLow
	}
}
