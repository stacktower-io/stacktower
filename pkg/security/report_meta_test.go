package security

import (
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/graph"
)

func testReport() *Report {
	r := NewReport(2)
	r.AddFinding(Finding{
		ID:         "GHSA-1234",
		Aliases:    []string{"CVE-2024-0001"},
		Package:    "left-pad",
		Version:    "1.0.0",
		Ecosystem:  "npm",
		Summary:    "test vuln",
		Severity:   SeverityHigh,
		References: []string{"https://example.com/advisory"},
	})
	r.VulnerableDeps = 1
	return r
}

func TestStoreReport_RoundTrip(t *testing.T) {
	g := dag.New(nil)
	StoreReport(g, testReport())

	got := ReportFromMeta(g)
	if got == nil {
		t.Fatal("ReportFromMeta returned nil")
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "GHSA-1234" {
		t.Errorf("unexpected findings: %+v", got.Findings)
	}
	if got.Findings[0].Aliases[0] != "CVE-2024-0001" {
		t.Errorf("aliases lost: %+v", got.Findings[0])
	}
	if got.SeveritySummary[SeverityHigh] != 1 {
		t.Errorf("severity summary lost: %v", got.SeveritySummary)
	}
}

func TestStoreReport_SurvivesGraphSerialization(t *testing.T) {
	g := dag.New(nil)
	if err := g.AddNode(dag.Node{ID: "root"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddNode(dag.Node{ID: "left-pad"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddEdge(dag.Edge{From: "root", To: "left-pad"}); err != nil {
		t.Fatal(err)
	}
	StoreReport(g, testReport())

	data, err := graph.MarshalGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	gj, err := graph.UnmarshalGraph(data)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := graph.ToDAG(gj)
	if err != nil {
		t.Fatal(err)
	}

	got := ReportFromMeta(restored)
	if got == nil {
		t.Fatal("report did not survive serialization round-trip")
	}
	if got.Findings[0].ID != "GHSA-1234" {
		t.Errorf("unexpected finding: %+v", got.Findings[0])
	}
}

func TestReportFromMeta_Absent(t *testing.T) {
	g := dag.New(nil)
	if got := ReportFromMeta(g); got != nil {
		t.Errorf("expected nil for graph without report, got %+v", got)
	}

	g.Meta()[MetaVulnFindings] = "not json"
	if got := ReportFromMeta(g); got != nil {
		t.Errorf("expected nil for malformed report, got %+v", got)
	}
}

func TestStripVulnData_RemovesStoredReport(t *testing.T) {
	g := dag.New(nil)
	if err := g.AddNode(dag.Node{ID: "left-pad", Meta: dag.Metadata{MetaVulnSeverity: "high"}}); err != nil {
		t.Fatal(err)
	}
	StoreReport(g, testReport())

	StripVulnData(g)

	if got := ReportFromMeta(g); got != nil {
		t.Error("stored report should be removed by StripVulnData")
	}
	for _, n := range g.Nodes() {
		if _, ok := n.Meta[MetaVulnSeverity]; ok {
			t.Error("node severity should be removed by StripVulnData")
		}
	}
}
