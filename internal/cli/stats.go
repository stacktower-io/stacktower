package cli

import (
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps/metadata"
	"github.com/stacktower-io/stacktower/pkg/core/render/tower/feature"
	"github.com/stacktower-io/stacktower/pkg/graph"
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

	cmd.Flags().StringVarP(&format, "format", "f", FormatText, "output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (stdout if omitted)")

	return cmd
}

// statsJSON is the stable wire format for `stats -f json`. It intentionally
// re-groups the flat ui.StatsReport fields into nested overview/maintenance/
// licenses/vulnerabilities sections — this is the public schema that
// downstream scripts consume, so we decouple it from the in-memory
// StatsReport used by the terminal renderer. If you add fields, update both
// types and add a test case covering the new JSON shape.
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
		return WrapSystemError(err, "failed to load graph", "Check that the file exists and contains valid graph JSON.")
	}

	graphStats := dag.ComputeStats(g)

	root := dag.FindRoot(g)
	rootVersion := ui.NodeVersion(g, root)
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

	if md := analyzeMaintenance(g, root); md.HasData {
		report.HasMaintenanceData = true
		report.SingleMaintainerCount = md.SingleMaintainerCount
		report.Brittle = md.Brittle
		report.Archived = md.Archived
		report.MedianLastCommitDays = md.MedianLastCommitDays

		// SingleMaintainerPct is derived from the overall dependency count,
		// not from within analyzeMaintenance, so it stays here.
		if report.TotalPackages > 1 {
			depCount := report.TotalPackages - 1 // exclude root
			if depCount > 0 {
				report.SingleMaintainerPct = float64(report.SingleMaintainerCount) / float64(depCount) * 100
			}
		}
	}

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

	writers := map[string]func(io.Writer) error{
		FormatJSON: func(w io.Writer) error { return writeStatsJSON(w, report) },
		FormatText: func(w io.Writer) error { ui.WriteStats(w, report); return nil },
	}
	if err := writeFormatted(output, format, writers); err != nil {
		return err
	}

	if output != "" {
		ui.PrintNewline()
		ui.PrintSuccess("Stats written")
		ui.PrintFile(output)
	}
	return nil
}

func writeStatsJSON(w io.Writer, r ui.StatsReport) error {
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

	return encodeJSON(w, out)
}

// maintenanceData is the pure-function output of analyzeMaintenance. It is
// merged into the caller's ui.StatsReport by runStats. Keeping the analysis
// separate from mutation makes it trivially testable.
type maintenanceData struct {
	HasData               bool
	SingleMaintainerCount int
	Brittle               []string
	Archived              []string
	MedianLastCommitDays  int
}

// analyzeMaintenance walks the graph and extracts maintenance signals from
// package metadata (single-maintainer packages, archived repos, brittle
// packages, median days since last commit). It never mutates inputs.
func analyzeMaintenance(g *dag.DAG, root string) maintenanceData {
	var md maintenanceData
	var commitDays []int

	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == graph.ProjectRootNodeID {
			continue
		}
		if n.Meta == nil {
			continue
		}

		maintainers := feature.CountMaintainers(n.Meta[metadata.RepoMaintainers])
		if maintainers == 1 {
			md.SingleMaintainerCount++
			md.HasData = true
		}

		if archived, _ := n.Meta[metadata.RepoArchived].(bool); archived {
			md.Archived = append(md.Archived, n.ID)
			md.HasData = true
		}

		if feature.IsBrittle(n) {
			md.Brittle = append(md.Brittle, n.ID)
			md.HasData = true
		}

		lastCommit := feature.ParseDate(n.Meta[metadata.RepoLastCommit])
		if !lastCommit.IsZero() {
			days := int(time.Since(lastCommit).Hours() / 24)
			commitDays = append(commitDays, days)
			md.HasData = true
		}
	}

	if n := len(commitDays); n > 0 {
		sort.Ints(commitDays)
		if n%2 == 0 {
			md.MedianLastCommitDays = (commitDays[n/2-1] + commitDays[n/2]) / 2
		} else {
			md.MedianLastCommitDays = commitDays[n/2]
		}
	}

	return md
}

func collectVulnData(g *dag.DAG, root string, r *ui.StatsReport) {
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == root || n.ID == graph.ProjectRootNodeID {
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
