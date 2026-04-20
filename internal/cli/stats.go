package cli

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps/metadata"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/feature"
	"github.com/stacktower-io/stacktower/pkg/security"
)

func (c *CLI) statsCommand() *cobra.Command {
	var (
		format string
		output string
	)

	cmd := &cobra.Command{
		Use:   "stats [graph.json|-]",
		Short: "Show dependency health report",
		Long: `Produce a structured dependency health report from a parsed graph.

Answers: "How healthy is my dependency tree?" by analyzing package counts,
maintenance signals, license compliance, vulnerabilities, and load-bearing packages.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runStats(args[0], format, output)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (stdout if empty)")

	return cmd
}

type statsJSON struct {
	Root     string `json:"root"`
	Version  string `json:"version"`
	Language string `json:"language"`

	Overview statsOverviewJSON `json:"overview"`

	Maintenance *statsMaintenanceJSON `json:"maintenance,omitempty"`
	Licenses    *statsLicensesJSON    `json:"licenses,omitempty"`
	Vulns       *statsVulnsJSON       `json:"vulnerabilities,omitempty"`
	LoadBearing []statsLoadJSON       `json:"load_bearing,omitempty"`
}

type statsOverviewJSON struct {
	TotalPackages int `json:"total_packages"`
	TotalEdges    int `json:"total_edges"`
	MaxDepth      int `json:"max_depth"`
	Direct        int `json:"direct"`
	Transitive    int `json:"transitive"`
}

type statsMaintenanceJSON struct {
	SingleMaintainerCount int      `json:"single_maintainer_count"`
	SingleMaintainerPct   float64  `json:"single_maintainer_pct"`
	Brittle               []string `json:"brittle"`
	Archived              []string `json:"archived"`
	MedianLastCommitDays  int      `json:"median_last_commit_days"`
}

type statsLicensesJSON struct {
	Summary   map[string]int `json:"summary"`
	Breakdown map[string]int `json:"breakdown"`
	Compliant bool           `json:"compliant"`
}

type statsVulnsJSON struct {
	Critical int                 `json:"critical"`
	High     int                 `json:"high"`
	Medium   int                 `json:"medium"`
	Low      int                 `json:"low"`
	Affected []statsAffectedJSON `json:"affected"`
}

type statsAffectedJSON struct {
	Package  string `json:"package"`
	Severity string `json:"severity"`
}

type statsLoadJSON struct {
	Package     string `json:"package"`
	ReverseDeps int    `json:"reverse_deps"`
}

func (c *CLI) runStats(input, format, output string) error {
	g, err := loadGraph(input)
	if err != nil {
		return WrapSystemError(err, "failed to load graph", "")
	}

	graphStats := dag.ComputeStats(g)

	root := dag.FindRoot(g)
	rootVersion := nodeVersion(g, root)
	language, _ := g.Meta()["language"].(string)

	report := ui.StatsReport{
		Root:           root,
		Version:        rootVersion,
		Language:       language,
		TotalPackages:  graphStats.NodeCount,
		TotalEdges:     graphStats.EdgeCount,
		MaxDepth:       graphStats.MaxDepth,
		DirectDeps:     graphStats.DirectDeps,
		TransitiveDeps: graphStats.TransitiveDeps,
	}

	// Load-bearing
	for _, lb := range graphStats.LoadBearing {
		report.LoadBearing = append(report.LoadBearing, ui.LoadBearingEntry{
			Package:     lb.ID,
			ReverseDeps: lb.ReverseDeps,
		})
	}

	// Maintenance analysis from node metadata
	report.HasMaintenanceData = collectMaintenanceData(g, root, &report)

	// License analysis
	licReport := security.AnalyzeLicenses(g)
	if licReport != nil && licReport.TotalDeps > 0 {
		report.HasLicenseData = true
		report.Compliant = licReport.Compliant
		report.LicenseSummary = map[string]int{}
		report.LicenseBreakdown = map[string]int{}

		for lic, pkgs := range licReport.Licenses {
			report.LicenseBreakdown[lic] = len(pkgs)
		}
		report.LicenseSummary["copyleft"] = len(licReport.Copyleft)
		report.LicenseSummary["weak-copyleft"] = len(licReport.WeakCopyleft)
		report.LicenseSummary["proprietary"] = len(licReport.Proprietary)
		report.LicenseSummary["unknown"] = len(licReport.Unknown)
		totalFlagged := len(licReport.Copyleft) + len(licReport.WeakCopyleft) +
			len(licReport.Proprietary) + len(licReport.Unknown)
		report.LicenseSummary["permissive"] = licReport.TotalDeps - totalFlagged
	}

	// Vulnerability data from node metadata (already annotated during parse --security-scan)
	collectVulnData(g, root, &report)

	w := os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return WrapSystemError(err, "failed to create output file", "")
		}
		defer f.Close()
		w = f
	}

	switch format {
	case "json":
		return writeStatsJSON(w, report)
	default:
		ui.WriteStats(w, report)
		return nil
	}
}

func writeStatsJSON(w *os.File, r ui.StatsReport) error {
	out := statsJSON{
		Root:     r.Root,
		Version:  r.Version,
		Language: r.Language,
		Overview: statsOverviewJSON{
			TotalPackages: r.TotalPackages,
			TotalEdges:    r.TotalEdges,
			MaxDepth:      r.MaxDepth,
			Direct:        r.DirectDeps,
			Transitive:    r.TransitiveDeps,
		},
	}

	if r.HasMaintenanceData {
		out.Maintenance = &statsMaintenanceJSON{
			SingleMaintainerCount: r.SingleMaintainerCount,
			SingleMaintainerPct:   r.SingleMaintainerPct,
			Brittle:               r.Brittle,
			Archived:              r.Archived,
			MedianLastCommitDays:  r.MedianLastCommitDays,
		}
	}

	if r.HasLicenseData {
		out.Licenses = &statsLicensesJSON{
			Summary:   r.LicenseSummary,
			Breakdown: r.LicenseBreakdown,
			Compliant: r.Compliant,
		}
	}

	if r.HasVulnData {
		affected := make([]statsAffectedJSON, len(r.VulnAffected))
		for i, v := range r.VulnAffected {
			affected[i] = statsAffectedJSON{Package: v.Package, Severity: v.Severity}
		}
		out.Vulns = &statsVulnsJSON{
			Critical: r.VulnCritical,
			High:     r.VulnHigh,
			Medium:   r.VulnMedium,
			Low:      r.VulnLow,
			Affected: affected,
		}
	}

	for _, lb := range r.LoadBearing {
		out.LoadBearing = append(out.LoadBearing, statsLoadJSON{
			Package:     lb.Package,
			ReverseDeps: lb.ReverseDeps,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func collectMaintenanceData(g *dag.DAG, root string, r *ui.StatsReport) bool {
	hasData := false
	var commitDays []int

	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == "__project__" {
			continue
		}
		if n.Meta == nil {
			continue
		}

		// Single-maintainer check
		maintainers := feature.CountMaintainers(n.Meta[metadata.RepoMaintainers])
		if maintainers == 1 {
			r.SingleMaintainerCount++
			hasData = true
		}

		// Archived check
		if archived, _ := n.Meta[metadata.RepoArchived].(bool); archived {
			r.Archived = append(r.Archived, n.ID)
			hasData = true
		}

		// Brittle check
		if feature.IsBrittle(n) {
			r.Brittle = append(r.Brittle, n.ID)
			hasData = true
		}

		// Last commit for median calculation
		lastCommit := feature.ParseDate(n.Meta[metadata.RepoLastCommit])
		if !lastCommit.IsZero() {
			days := int(time.Since(lastCommit).Hours() / 24)
			commitDays = append(commitDays, days)
			hasData = true
		}
	}

	if r.TotalPackages > 1 {
		depCount := r.TotalPackages - 1 // exclude root
		if depCount > 0 {
			r.SingleMaintainerPct = float64(r.SingleMaintainerCount) / float64(depCount) * 100
		}
	}

	if len(commitDays) > 0 {
		sort.Ints(commitDays)
		r.MedianLastCommitDays = commitDays[len(commitDays)/2]
	}

	return hasData
}

func collectVulnData(g *dag.DAG, root string, r *ui.StatsReport) {
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == "__project__" {
			continue
		}
		if n.Meta == nil {
			continue
		}

		sev, ok := n.Meta[security.MetaVulnSeverity].(string)
		if !ok || sev == "" {
			continue
		}

		r.HasVulnData = true
		switch security.Severity(sev) {
		case security.SeverityCritical:
			r.VulnCritical++
		case security.SeverityHigh:
			r.VulnHigh++
		case security.SeverityMedium:
			r.VulnMedium++
		case security.SeverityLow:
			r.VulnLow++
		}

		r.VulnAffected = append(r.VulnAffected, ui.VulnAffectedPkg{
			Package:  n.ID,
			Severity: sev,
		})
	}
}
