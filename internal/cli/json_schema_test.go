package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// TestStatsJSONSchema verifies that writeStatsJSON produces the expected
// top-level keys. This prevents accidental schema breakage for downstream
// tooling that consumes `stats -f json` output.
func TestStatsJSONSchema(t *testing.T) {
	report := ui.StatsReport{
		Root:           "flask",
		Version:        "3.0.0",
		Language:       "python",
		TotalPackages:  10,
		TotalEdges:     12,
		MaxDepth:       3,
		DirectDeps:     5,
		TransitiveDeps: 4,
	}

	var buf bytes.Buffer
	if err := writeStatsJSON(&buf, report); err != nil {
		t.Fatalf("writeStatsJSON error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	requiredKeys := []string{"root", "version", "language", "overview"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing required top-level key %q in stats JSON", key)
		}
	}

	// overview should have the expected subkeys
	var overview map[string]json.RawMessage
	if err := json.Unmarshal(raw["overview"], &overview); err != nil {
		t.Fatalf("failed to parse overview: %v", err)
	}
	overviewKeys := []string{"total_packages", "total_edges", "max_depth", "direct", "transitive"}
	for _, key := range overviewKeys {
		if _, ok := overview[key]; !ok {
			t.Errorf("missing key %q in overview", key)
		}
	}
}

// TestStatsJSONSchema_WithOptionalSections verifies that optional sections
// (maintenance, licenses, vulnerabilities, load_bearing) appear when populated.
func TestStatsJSONSchema_WithOptionalSections(t *testing.T) {
	report := ui.StatsReport{
		Root:                  "flask",
		Version:               "3.0.0",
		Language:              "python",
		TotalPackages:         10,
		TotalEdges:            12,
		MaxDepth:              3,
		DirectDeps:            5,
		TransitiveDeps:        4,
		HasMaintenanceData:    true,
		SingleMaintainerCount: 3,
		SingleMaintainerPct:   33.3,
		MedianLastCommitDays:  47,
		HasLicenseData:        true,
		Compliant:             true,
		LicenseSummary:        map[string]int{"permissive": 8, "unknown": 1},
		LicenseBreakdown:      map[string]int{"MIT": 5, "Apache-2.0": 3},
		HasVulnData:           true,
		VulnHigh:              1,
		VulnMedium:            2,
		VulnAffected:          []ui.VulnAffectedPkg{{Package: "werkzeug", Severity: "high"}},
		LoadBearing: []ui.LoadBearingEntry{
			{Package: "markupsafe", ReverseDeps: 3},
		},
	}

	var buf bytes.Buffer
	if err := writeStatsJSON(&buf, report); err != nil {
		t.Fatalf("writeStatsJSON error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	optionalKeys := []string{"maintenance", "licenses", "vulnerabilities", "load_bearing"}
	for _, key := range optionalKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected optional key %q in stats JSON when data is populated", key)
		}
	}
}

// TestDiffJSONSchema verifies that writeDiffJSON produces the expected
// top-level keys and structure.
func TestDiffJSONSchema(t *testing.T) {
	before := dag.New(nil)
	before.AddNode(dag.Node{ID: "app", Meta: dag.Metadata{"version": "1.0"}})
	before.AddNode(dag.Node{ID: "dep-a", Meta: dag.Metadata{"version": "1.0"}})
	before.AddEdge(dag.Edge{From: "app", To: "dep-a"})

	after := dag.New(nil)
	after.AddNode(dag.Node{ID: "app", Meta: dag.Metadata{"version": "1.0"}})
	after.AddNode(dag.Node{ID: "dep-a", Meta: dag.Metadata{"version": "2.0"}})
	after.AddNode(dag.Node{ID: "dep-b", Meta: dag.Metadata{"version": "1.0"}})
	after.AddEdge(dag.Edge{From: "app", To: "dep-a"})
	after.AddEdge(dag.Edge{From: "app", To: "dep-b"})

	d := dag.Diff(before, after)

	var buf bytes.Buffer
	if err := writeDiffJSON(&buf, d); err != nil {
		t.Fatalf("writeDiffJSON error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse diff JSON: %v", err)
	}

	requiredKeys := []string{"before", "after", "added", "removed", "updated", "unchanged", "new_vulns"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing required key %q in diff JSON", key)
		}
	}

	// Verify before/after have expected subkeys
	for _, side := range []string{"before", "after"} {
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(raw[side], &summary); err != nil {
			t.Fatalf("failed to parse %s: %v", side, err)
		}
		for _, key := range []string{"root", "version", "total"} {
			if _, ok := summary[key]; !ok {
				t.Errorf("missing key %q in %s summary", key, side)
			}
		}
	}

	// Verify actual diff content
	var result dag.DiffResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to deserialize DiffResult: %v", err)
	}
	if len(result.Added) != 1 || result.Added[0].ID != "dep-b" {
		t.Errorf("expected 1 added (dep-b), got %v", result.Added)
	}
	if len(result.Updated) != 1 || result.Updated[0].ID != "dep-a" {
		t.Errorf("expected 1 updated (dep-a), got %v", result.Updated)
	}
}

// TestErrNotLoggedIn verifies the sentinel error works with errors.Is
// through the CLIError wrapper.
func TestErrNotLoggedIn(t *testing.T) {
	err := WrapUserError(ErrNotLoggedIn, "not logged in", "Run 'stacktower github login' first.")

	if !errors.Is(err, ErrNotLoggedIn) {
		t.Error("errors.Is(err, ErrNotLoggedIn) should be true for wrapped sentinel")
	}

	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}
	if cliErr.Kind != ErrorKindUser {
		t.Errorf("Kind = %v, want %v", cliErr.Kind, ErrorKindUser)
	}
}

// TestExitCodeVuln verifies the vuln exit code constant value.
func TestExitCodeVuln(t *testing.T) {
	if ExitCodeVuln != 3 {
		t.Errorf("ExitCodeVuln = %d, want 3", ExitCodeVuln)
	}
}
