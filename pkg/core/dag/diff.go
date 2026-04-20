package dag

import "sort"

// DiffResult describes the differences between two dependency graphs.
type DiffResult struct {
	Before    DiffSummary
	After     DiffSummary
	Added     []DiffEntry
	Removed   []DiffEntry
	Updated   []DiffUpdate
	Unchanged int
	NewVulns  []DiffVuln
}

// DiffSummary captures the root and size of one side of the diff.
type DiffSummary struct {
	RootID      string
	RootVersion string
	NodeCount   int
}

// DiffEntry represents a package that was added or removed.
type DiffEntry struct {
	ID      string
	Version string
}

// DiffUpdate represents a package whose version changed between graphs.
type DiffUpdate struct {
	ID          string
	OldVersion  string
	NewVersion  string
	DepthChange int // positive = deeper, negative = shallower
}

// DiffVuln represents a package that gained (or escalated) a vulnerability.
type DiffVuln struct {
	ID          string
	Version     string
	Severity    string
	WasSeverity string // empty if the package was not vulnerable before
}

// Diff compares two DAGs and returns a structured diff result.
// Comparison is by node ID (package name); version changes within the same
// ID are reported as updates.
func Diff(before, after *DAG) *DiffResult {
	result := &DiffResult{
		Before: buildDiffSummary(before),
		After:  buildDiffSummary(after),
	}

	// Exclude roots — they are the packages being compared, not dependencies.
	excludeIDs := map[string]bool{
		result.Before.RootID: true,
		result.After.RootID:  true,
	}

	beforeNodes := collectDiffNodes(before, excludeIDs)
	afterNodes := collectDiffNodes(after, excludeIDs)

	// Added: in after but not in before
	for id, an := range afterNodes {
		if _, ok := beforeNodes[id]; !ok {
			result.Added = append(result.Added, DiffEntry{
				ID:      id,
				Version: metaVersion(an),
			})
		}
	}

	// Removed: in before but not in after
	for id, bn := range beforeNodes {
		if _, ok := afterNodes[id]; !ok {
			result.Removed = append(result.Removed, DiffEntry{
				ID:      id,
				Version: metaVersion(bn),
			})
		}
	}

	// Updated / Unchanged: in both
	for id, bn := range beforeNodes {
		an, ok := afterNodes[id]
		if !ok {
			continue
		}
		oldV := metaVersion(bn)
		newV := metaVersion(an)
		if oldV != newV {
			result.Updated = append(result.Updated, DiffUpdate{
				ID:         id,
				OldVersion: oldV,
				NewVersion: newV,
			})
		} else {
			result.Unchanged++
		}
	}

	// Sort for deterministic output
	sort.Slice(result.Added, func(i, j int) bool { return result.Added[i].ID < result.Added[j].ID })
	sort.Slice(result.Removed, func(i, j int) bool { return result.Removed[i].ID < result.Removed[j].ID })
	sort.Slice(result.Updated, func(i, j int) bool { return result.Updated[i].ID < result.Updated[j].ID })

	// Detect new vulnerabilities
	result.NewVulns = detectNewVulns(beforeNodes, afterNodes)

	return result
}

func buildDiffSummary(g *DAG) DiffSummary {
	root := FindRoot(g)
	return DiffSummary{
		RootID:      root,
		RootVersion: metaVersion(nodeOrNil(g, root)),
		NodeCount:   g.NodeCount(),
	}
}

func collectDiffNodes(g *DAG, exclude map[string]bool) map[string]*Node {
	m := make(map[string]*Node)
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" || exclude[n.ID] {
			continue
		}
		m[n.ID] = n
	}
	return m
}

func metaVersion(n *Node) string {
	if n == nil || n.Meta == nil {
		return ""
	}
	v, _ := n.Meta["version"].(string)
	return v
}

func nodeOrNil(g *DAG, id string) *Node {
	n, _ := g.Node(id)
	return n
}

const metaVulnSeverity = "vuln_severity"

func detectNewVulns(beforeNodes, afterNodes map[string]*Node) []DiffVuln {
	var vulns []DiffVuln
	for id, an := range afterNodes {
		afterSev := nodeVulnSeverity(an)
		if afterSev == "" {
			continue
		}

		bn, existed := beforeNodes[id]
		beforeSev := ""
		if existed {
			beforeSev = nodeVulnSeverity(bn)
		}

		// New vuln: package is new with vuln, or package existed without vuln,
		// or package had lower severity.
		if !existed || beforeSev == "" || beforeSev != afterSev {
			vulns = append(vulns, DiffVuln{
				ID:          id,
				Version:     metaVersion(an),
				Severity:    afterSev,
				WasSeverity: beforeSev,
			})
		}
	}

	sort.Slice(vulns, func(i, j int) bool { return vulns[i].ID < vulns[j].ID })
	return vulns
}

func nodeVulnSeverity(n *Node) string {
	if n == nil || n.Meta == nil {
		return ""
	}
	s, _ := n.Meta[metaVulnSeverity].(string)
	return s
}
