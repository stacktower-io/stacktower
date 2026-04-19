package dag

import (
	"slices"
	"testing"
)

func makeDiffGraph(nodes []struct{ id, version string }, edges [][2]string) *DAG {
	g := New(nil)
	for _, n := range nodes {
		meta := Metadata{"version": n.version}
		g.AddNode(Node{ID: n.id, Row: 0, Meta: meta})
	}
	for _, e := range edges {
		g.AddEdge(Edge{From: e[0], To: e[1]})
	}
	return g
}

func TestDiff_Identical(t *testing.T) {
	nodes := []struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}}
	edges := [][2]string{{"A", "B"}}
	g1 := makeDiffGraph(nodes, edges)
	g2 := makeDiffGraph(nodes, edges)

	d := Diff(g1, g2)
	if len(d.Added) != 0 {
		t.Errorf("expected no added, got %d", len(d.Added))
	}
	if len(d.Removed) != 0 {
		t.Errorf("expected no removed, got %d", len(d.Removed))
	}
	if len(d.Updated) != 0 {
		t.Errorf("expected no updated, got %d", len(d.Updated))
	}
	if d.Unchanged != 2 {
		t.Errorf("expected 2 unchanged, got %d", d.Unchanged)
	}
}

func TestDiff_AddedOnly(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}},
		nil,
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}},
		[][2]string{{"A", "B"}},
	)

	d := Diff(before, after)
	if len(d.Added) != 1 || d.Added[0].ID != "B" {
		t.Errorf("expected B added, got %v", d.Added)
	}
	if len(d.Removed) != 0 {
		t.Errorf("expected no removed, got %d", len(d.Removed))
	}
}

func TestDiff_RemovedOnly(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}},
		[][2]string{{"A", "B"}},
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}},
		nil,
	)

	d := Diff(before, after)
	if len(d.Removed) != 1 || d.Removed[0].ID != "B" {
		t.Errorf("expected B removed, got %v", d.Removed)
	}
}

func TestDiff_VersionUpdate(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}},
		[][2]string{{"A", "B"}},
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "3.0"}},
		[][2]string{{"A", "B"}},
	)

	d := Diff(before, after)
	if len(d.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(d.Updated))
	}
	if d.Updated[0].ID != "B" || d.Updated[0].OldVersion != "2.0" || d.Updated[0].NewVersion != "3.0" {
		t.Errorf("unexpected update: %+v", d.Updated[0])
	}
	if d.Unchanged != 1 {
		t.Errorf("expected 1 unchanged (A), got %d", d.Unchanged)
	}
}

func TestDiff_Mixed(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}, {"C", "1.0"}},
		[][2]string{{"A", "B"}, {"A", "C"}},
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "3.0"}, {"D", "1.0"}},
		[][2]string{{"A", "B"}, {"A", "D"}},
	)

	d := Diff(before, after)
	if len(d.Added) != 1 || d.Added[0].ID != "D" {
		t.Errorf("expected D added, got %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != "C" {
		t.Errorf("expected C removed, got %v", d.Removed)
	}
	if len(d.Updated) != 1 || d.Updated[0].ID != "B" {
		t.Errorf("expected B updated, got %v", d.Updated)
	}
}

func TestDiff_CompletelyDifferent(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "1.0"}},
		[][2]string{{"A", "B"}},
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"X", "1.0"}, {"Y", "1.0"}},
		[][2]string{{"X", "Y"}},
	)

	d := Diff(before, after)
	if len(d.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(d.Added))
	}
	if len(d.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(d.Removed))
	}
	if d.Unchanged != 0 {
		t.Errorf("expected 0 unchanged, got %d", d.Unchanged)
	}
}

func TestDiff_NewVulns(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"A", "1.0"}, {"B", "1.0"}},
		[][2]string{{"A", "B"}},
	)
	afterNodes := []struct{ id, version string }{{"A", "1.0"}, {"B", "2.0"}, {"C", "1.0"}}
	after := makeDiffGraph(afterNodes, [][2]string{{"A", "B"}, {"A", "C"}})

	// Annotate B with a new vuln and C (new dep) with a vuln
	bn, _ := after.Node("B")
	bn.Meta["vuln_severity"] = "high"
	cn, _ := after.Node("C")
	cn.Meta["vuln_severity"] = "medium"

	d := Diff(before, after)

	if len(d.NewVulns) != 2 {
		t.Fatalf("expected 2 new vulns, got %d", len(d.NewVulns))
	}

	ids := []string{d.NewVulns[0].ID, d.NewVulns[1].ID}
	slices.Sort(ids)
	if ids[0] != "B" || ids[1] != "C" {
		t.Errorf("expected B and C, got %v", ids)
	}
}

func TestDiff_Summary(t *testing.T) {
	before := makeDiffGraph(
		[]struct{ id, version string }{{"flask", "2.0.0"}, {"click", "8.0"}},
		[][2]string{{"flask", "click"}},
	)
	after := makeDiffGraph(
		[]struct{ id, version string }{{"flask", "3.0.0"}, {"click", "8.1"}},
		[][2]string{{"flask", "click"}},
	)

	d := Diff(before, after)
	if d.Before.RootID != "flask" || d.Before.RootVersion != "2.0.0" {
		t.Errorf("before summary: %+v", d.Before)
	}
	if d.After.RootID != "flask" || d.After.RootVersion != "3.0.0" {
		t.Errorf("after summary: %+v", d.After)
	}
}
