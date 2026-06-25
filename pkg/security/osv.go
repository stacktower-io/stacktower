package security

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stacktower-io/stacktower/pkg/integrations"
	"github.com/stacktower-io/stacktower/pkg/integrations/osv"
	"github.com/stacktower-io/stacktower/pkg/observability"
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

	// Skip dependencies without a resolved version: a versionless OSV query
	// returns vulnerabilities across ALL versions of the package, producing
	// false positives. Record them so callers can surface the gap.
	scannable := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		if strings.TrimSpace(dep.Version) == "" {
			report.Unscanned = append(report.Unscanned, dep.Name)
			continue
		}
		scannable = append(scannable, dep)
	}
	deps = scannable
	if len(deps) == 0 {
		observability.Security().OnScanComplete(ctx, ecosystem, 0, time.Since(start), nil)
		return report, nil
	}

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
		// Defense in depth: OSV filters by version server-side, but stale
		// cached responses or incomplete batch data can slip through. Drop
		// findings whose affected data definitively excludes our version.
		if !isVersionAffected(v, dep) {
			continue
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
	if merged.DatabaseSpecific.Severity == "" {
		merged.DatabaseSpecific.Severity = full.DatabaseSpecific.Severity
	}
	return merged
}

// isVersionAffected reports whether dep.Version falls within the
// vulnerability's affected version set for dep's package and ecosystem.
//
// The check is conservative: it only returns false when affected data for
// this exact package exists and definitively excludes the version. Missing
// or unparseable data keeps the finding.
func isVersionAffected(v osv.Vulnerability, dep Dependency) bool {
	if dep.Version == "" || len(v.Affected) == 0 {
		return true
	}

	depVer := integrations.ParseSemver(dep.Version)

	matchedPackage := false
	for _, aff := range v.Affected {
		if !strings.EqualFold(aff.Package.Name, dep.Name) ||
			!strings.EqualFold(aff.Package.Ecosystem, dep.Ecosystem) {
			continue
		}
		matchedPackage = true

		// Explicit affected version list.
		for _, ver := range aff.Versions {
			if strings.TrimPrefix(ver, "v") == strings.TrimPrefix(dep.Version, "v") {
				return true
			}
		}

		// Range events. GIT ranges use commit hashes, which we can't compare.
		for _, r := range aff.Ranges {
			if r.Type == "GIT" {
				return true
			}
			if !depVer.Valid {
				// Can't compare non-semver versions against ranges: keep.
				return true
			}
			if rangeContainsVersion(r.Events, depVer) {
				return true
			}
		}
	}

	// No affected entry mentions this package at all: trust the server-side
	// match and keep the finding.
	return !matchedPackage
}

// rangeContainsVersion walks OSV range events (sorted ascending per the OSV
// spec) and reports whether ver falls in an affected interval.
func rangeContainsVersion(events []osv.Event, ver integrations.SemanticVersion) bool {
	affected := false
	for _, e := range events {
		if e.Introduced != "" {
			if e.Introduced == "0" {
				affected = true
			} else if iv := integrations.ParseSemver(e.Introduced); !iv.Valid || ver.Compare(iv) >= 0 {
				// Unparseable boundary: assume affected (conservative).
				affected = true
			}
		}
		if e.Fixed != "" {
			if fv := integrations.ParseSemver(e.Fixed); fv.Valid && ver.Compare(fv) >= 0 {
				affected = false
			}
		}
		if e.LastAffected != "" {
			if lv := integrations.ParseSemver(e.LastAffected); lv.Valid && ver.Compare(lv) > 0 {
				affected = false
			}
		}
	}
	return affected
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
//
// Precedence:
//  1. CVSS entries (numeric scores, computed CVSS v3 base scores, or
//     CVSS v4 vector heuristics) — the highest severity across entries wins.
//  2. The database_specific severity string (GHSA: CRITICAL/HIGH/MODERATE/LOW).
//  3. SeverityUnknown.
func extractSeverity(v osv.Vulnerability) Severity {
	best := SeverityUnknown
	for _, s := range v.Severity {
		if sev := severityFromEntry(s); sev.Weight() > best.Weight() {
			best = sev
		}
	}
	if best != SeverityUnknown {
		return best
	}

	switch strings.ToLower(strings.TrimSpace(v.DatabaseSpecific.Severity)) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "moderate", "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	}

	return SeverityUnknown
}

// severityFromEntry maps a single OSV severity entry to a Severity.
func severityFromEntry(s osv.SeverityEntry) Severity {
	score := strings.TrimSpace(s.Score)
	if score == "" {
		return SeverityUnknown
	}

	// Some feeds provide a bare numeric score (e.g. "7.5").
	if f, err := strconv.ParseFloat(score, 64); err == nil {
		return severityFromScore(f)
	}

	vector := strings.ToUpper(score)
	switch {
	case strings.HasPrefix(vector, "CVSS:3"):
		if f, ok := cvss3BaseScore(vector); ok {
			return severityFromScore(f)
		}
	case strings.HasPrefix(vector, "CVSS:4"):
		return severityFromV4Vector(vector)
	}

	// Unrecognized format: best-effort component heuristic.
	return severityFromVectorHeuristic(vector)
}

// severityFromScore maps a numeric CVSS score to a severity per the standard
// ranges: 0.1-3.9 = Low, 4.0-6.9 = Medium, 7.0-8.9 = High, 9.0-10.0 = Critical.
func severityFromScore(score float64) Severity {
	switch {
	case score >= 9.0:
		return SeverityCritical
	case score >= 7.0:
		return SeverityHigh
	case score >= 4.0:
		return SeverityMedium
	case score > 0:
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

// cvss3BaseScore computes the CVSS v3.x base score from a vector string like
// "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H". Returns false if required
// base metrics are missing or invalid.
func cvss3BaseScore(vector string) (float64, bool) {
	metrics := make(map[string]string, 8)
	for _, part := range strings.Split(vector, "/") {
		if k, v, ok := strings.Cut(part, ":"); ok {
			metrics[k] = v
		}
	}

	av, ok1 := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}[metrics["AV"]]
	ac, ok2 := map[string]float64{"L": 0.77, "H": 0.44}[metrics["AC"]]
	ui, ok3 := map[string]float64{"N": 0.85, "R": 0.62}[metrics["UI"]]

	scope, okScope := metrics["S"]
	scopeChanged := scope == "C"
	if !okScope || (scope != "U" && scope != "C") {
		return 0, false
	}

	prTable := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	if scopeChanged {
		prTable = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	}
	pr, ok4 := prTable[metrics["PR"]]

	cia := map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	c, ok5 := cia[metrics["C"]]
	i, ok6 := cia[metrics["I"]]
	a, ok7 := cia[metrics["A"]]

	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if scopeChanged {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0, true
	}

	exploitability := 8.22 * av * ac * pr * ui
	score := impact + exploitability
	if scopeChanged {
		score *= 1.08
	}
	return cvssRoundup(math.Min(score, 10)), true
}

// cvssRoundup implements the CVSS v3.1 Roundup function: the smallest number
// with one decimal place that is >= the input.
func cvssRoundup(x float64) float64 {
	scaled := int(math.Round(x * 100000))
	if scaled%10000 == 0 {
		return float64(scaled) / 100000
	}
	return (math.Floor(float64(scaled)/10000) + 1) / 10
}

// severityFromV4Vector estimates severity from a CVSS v4.0 vector using the
// vulnerable-system impact metrics (VC/VI/VA). Computing the exact v4 score
// requires the full MacroVector lookup table; this approximation keeps the
// standard thresholds directionally correct.
func severityFromV4Vector(v string) Severity {
	highImpacts := 0
	for _, m := range []string{"/VC:H", "/VI:H", "/VA:H"} {
		if strings.Contains(v, m) {
			highImpacts++
		}
	}
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

// severityFromVectorHeuristic estimates severity from unrecognized vector
// formats by scanning for CVSS-style impact components. Returns
// SeverityUnknown when nothing recognizable is present.
func severityFromVectorHeuristic(v string) Severity {
	if !strings.Contains(v, ":") {
		return SeverityUnknown
	}

	highImpacts := 0
	for _, m := range []string{"/C:H", "/I:H", "/A:H"} {
		if strings.Contains(v, m) {
			highImpacts++
		}
	}
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
	case networkAccess || lowComplexity || noPriv:
		return SeverityLow
	default:
		return SeverityUnknown
	}
}
