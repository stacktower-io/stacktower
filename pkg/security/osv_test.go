package security

import (
	"context"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/integrations/osv"
)

func TestScanSkipsVersionlessDeps(t *testing.T) {
	// All deps lack versions → no network call is made, and the report lists
	// them as unscanned instead of producing cross-version false positives.
	s := NewOSVScanner(nil)
	report, err := s.Scan(context.Background(), []Dependency{
		{Name: "lodash", Ecosystem: "npm"},
		{Name: "express", Version: "  ", Ecosystem: "npm"},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("Findings = %d, want 0", len(report.Findings))
	}
	if len(report.Unscanned) != 2 {
		t.Errorf("Unscanned = %v, want [lodash express]", report.Unscanned)
	}
	if report.TotalDeps != 2 {
		t.Errorf("TotalDeps = %d, want 2", report.TotalDeps)
	}
}

func TestCVSS3BaseScore(t *testing.T) {
	tests := []struct {
		vector string
		want   float64
	}{
		// Log4Shell (CVE-2021-44228): published score 10.0
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10.0},
		// Classic network RCE: published score 9.8
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
		// Information disclosure: published score 5.3
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N", 5.3},
		// Low-severity example: published score 3.1
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:N/I:N/A:L", 3.1},
		// No impact at all: score 0
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0.0},
		// CVSS 3.0 vectors use the same formula
		{"CVSS:3.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
	}

	for _, tt := range tests {
		t.Run(tt.vector, func(t *testing.T) {
			got, ok := cvss3BaseScore(tt.vector)
			if !ok {
				t.Fatalf("cvss3BaseScore(%q) not parseable", tt.vector)
			}
			if got != tt.want {
				t.Errorf("cvss3BaseScore(%q) = %v, want %v", tt.vector, got, tt.want)
			}
		})
	}

	// Malformed vectors must be rejected, not misclassified.
	for _, bad := range []string{
		"CVSS:3.1/AV:N/AC:L",                           // missing metrics
		"CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", // invalid AV value
	} {
		if _, ok := cvss3BaseScore(bad); ok {
			t.Errorf("cvss3BaseScore(%q) should not parse", bad)
		}
	}
}

func TestIsVersionAffected(t *testing.T) {
	dep := Dependency{Name: "werkzeug", Version: "2.0.0", Ecosystem: "PyPI"}

	tests := []struct {
		name string
		vuln osv.Vulnerability
		want bool
	}{
		{
			name: "no affected data keeps finding",
			vuln: osv.Vulnerability{ID: "X"},
			want: true,
		},
		{
			name: "version inside introduced/fixed range",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "0"}, {Fixed: "2.1.0"},
				}}},
			}}},
			want: true,
		},
		{
			name: "version above fixed boundary is excluded",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "0"}, {Fixed: "1.0.1"},
				}}},
			}}},
			want: false,
		},
		{
			name: "version below introduced boundary is excluded",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "3.0.0"}, {Fixed: "3.0.5"},
				}}},
			}}},
			want: false,
		},
		{
			name: "explicit version list match",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package:  osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Versions: []string{"1.0.0", "2.0.0"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "3.0.0"},
				}}},
			}}},
			want: true,
		},
		{
			name: "last_affected boundary excludes higher versions",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "0"}, {LastAffected: "1.9.9"},
				}}},
			}}},
			want: false,
		},
		{
			name: "different package in affected data keeps finding",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "flask", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "ECOSYSTEM", Events: []osv.Event{
					{Introduced: "0"}, {Fixed: "1.0.0"},
				}}},
			}}},
			want: true,
		},
		{
			name: "git ranges cannot be compared, keep finding",
			vuln: osv.Vulnerability{Affected: []osv.Affected{{
				Package: osv.AffectedPackage{Name: "werkzeug", Ecosystem: "PyPI"},
				Ranges: []osv.Range{{Type: "GIT", Events: []osv.Event{
					{Introduced: "abc123"}, {Fixed: "def456"},
				}}},
			}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVersionAffected(tt.vuln, dep); got != tt.want {
				t.Errorf("isVersionAffected() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractSeverity(t *testing.T) {
	tests := []struct {
		name string
		vuln osv.Vulnerability
		want Severity
	}{
		{
			name: "cvss v3 critical vector",
			vuln: osv.Vulnerability{
				ID:       "CVE-2021-44228",
				Severity: []osv.SeverityEntry{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H"}},
			},
			want: SeverityCritical,
		},
		{
			name: "cvss v3 medium vector",
			vuln: osv.Vulnerability{
				ID:       "CVE-X",
				Severity: []osv.SeverityEntry{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N"}},
			},
			want: SeverityMedium,
		},
		{
			name: "cvss v3 low vector",
			vuln: osv.Vulnerability{
				ID:       "CVE-X",
				Severity: []osv.SeverityEntry{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:N/I:N/A:L"}},
			},
			want: SeverityLow,
		},
		{
			name: "bare numeric score",
			vuln: osv.Vulnerability{
				ID:       "CVE-X",
				Severity: []osv.SeverityEntry{{Type: "CVSS_V3", Score: "7.5"}},
			},
			want: SeverityHigh,
		},
		{
			name: "cvss v4 vector",
			vuln: osv.Vulnerability{
				ID:       "CVE-X",
				Severity: []osv.SeverityEntry{{Type: "CVSS_V4", Score: "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N"}},
			},
			want: SeverityCritical,
		},
		{
			name: "highest severity across entries wins",
			vuln: osv.Vulnerability{
				ID: "CVE-X",
				Severity: []osv.SeverityEntry{
					{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N"}, // medium
					{Type: "CVSS_V3", Score: "9.1"},                                          // critical
				},
			},
			want: SeverityCritical,
		},
		{
			name: "ghsa database_specific severity",
			vuln: osv.Vulnerability{
				ID:               "GHSA-xxxx-yyyy-zzzz",
				DatabaseSpecific: osv.DatabaseSpecific{Severity: "MODERATE"},
			},
			want: SeverityMedium,
		},
		{
			name: "ghsa without any severity data is unknown, not medium",
			vuln: osv.Vulnerability{ID: "GHSA-xxxx-yyyy-zzzz"},
			want: SeverityUnknown,
		},
		{
			name: "no severity data",
			vuln: osv.Vulnerability{ID: "CVE-X"},
			want: SeverityUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSeverity(tt.vuln); got != tt.want {
				t.Errorf("extractSeverity() = %v, want %v", got, tt.want)
			}
		})
	}
}
