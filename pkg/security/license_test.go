package security

import (
	"testing"

	"github.com/matzehuels/stacktower/pkg/core/dag"
)

func TestClassifyLicense(t *testing.T) {
	tests := []struct {
		license string
		want    LicenseRisk
	}{
		// Permissive
		{"MIT", LicenseRiskPermissive},
		{"MIT License", LicenseRiskPermissive},
		{"ISC", LicenseRiskPermissive},
		{"BSD-2-Clause", LicenseRiskPermissive},
		{"BSD-3-Clause", LicenseRiskPermissive},
		{"Apache-2.0", LicenseRiskPermissive},
		{"Apache Software License", LicenseRiskPermissive},
		{"Unlicense", LicenseRiskPermissive},
		{"CC0-1.0", LicenseRiskPermissive},
		{"0BSD", LicenseRiskPermissive},
		{"Zlib", LicenseRiskPermissive},
		{"BSL-1.0", LicenseRiskPermissive},
		{"Python-2.0", LicenseRiskPermissive},
		{"PSF-2.0", LicenseRiskPermissive},

		// Weak copyleft
		{"LGPL-2.1", LicenseRiskWeakCopyleft},
		{"LGPL-3.0", LicenseRiskWeakCopyleft},
		{"MPL-2.0", LicenseRiskWeakCopyleft},
		{"EPL-2.0", LicenseRiskWeakCopyleft},
		{"CDDL-1.0", LicenseRiskWeakCopyleft},
		{"Mozilla Public License 2.0", LicenseRiskWeakCopyleft},
		// PyPI classifier format for LGPL (psycopg2-binary uses this)
		{"GNU Library or Lesser General Public License (LGPL)", LicenseRiskWeakCopyleft},

		// Copyleft
		{"GPL-2.0", LicenseRiskCopyleft},
		{"GPL-3.0", LicenseRiskCopyleft},
		{"AGPL-3.0", LicenseRiskCopyleft},
		{"GNU General Public License v3.0", LicenseRiskCopyleft},

		// Proprietary / Source-available
		{"SSPL-1.0", LicenseRiskProprietary},
		{"BUSL-1.1", LicenseRiskProprietary},
		{"Business Source License", LicenseRiskProprietary},
		{"Elastic-2.0", LicenseRiskProprietary},
		{"Commons-Clause", LicenseRiskProprietary},

		// Unknown
		{"", LicenseRiskUnknown},
		{"UNKNOWN", LicenseRiskUnknown},
		{"Custom", LicenseRiskUnknown},

		// Compound OR — least restrictive wins
		{"MIT OR Apache-2.0", LicenseRiskPermissive},
		{"MIT OR GPL-3.0", LicenseRiskPermissive},
		{"GPL-3.0 OR MIT", LicenseRiskPermissive},
		{"LGPL-2.1 OR GPL-3.0", LicenseRiskWeakCopyleft},

		// Compound AND — most restrictive wins
		{"MIT AND BSD-3-Clause", LicenseRiskPermissive},
		{"MIT AND GPL-3.0", LicenseRiskCopyleft},
		{"Apache-2.0 AND LGPL-2.1", LicenseRiskWeakCopyleft},
	}

	for _, tt := range tests {
		t.Run(tt.license, func(t *testing.T) {
			got := ClassifyLicense(tt.license)
			if got != tt.want {
				t.Errorf("ClassifyLicense(%q) = %q, want %q", tt.license, got, tt.want)
			}
		})
	}
}

func TestLicenseRisk_Weight(t *testing.T) {
	if LicenseRiskPermissive.Weight() >= LicenseRiskUnknown.Weight() {
		t.Error("permissive should be less than unknown")
	}
	if LicenseRiskUnknown.Weight() >= LicenseRiskWeakCopyleft.Weight() {
		t.Error("unknown should be less than weak-copyleft")
	}
	if LicenseRiskWeakCopyleft.Weight() >= LicenseRiskCopyleft.Weight() {
		t.Error("weak-copyleft should be less than copyleft")
	}
	if LicenseRiskCopyleft.Weight() >= LicenseRiskProprietary.Weight() {
		t.Error("copyleft should be less than proprietary")
	}
}

func TestLicenseRisk_IsFlagged(t *testing.T) {
	tests := []struct {
		risk LicenseRisk
		want bool
	}{
		{LicenseRiskPermissive, false},
		{LicenseRiskWeakCopyleft, true},
		{LicenseRiskCopyleft, true},
		{LicenseRiskProprietary, true},
		{LicenseRiskUnknown, true},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.risk.IsFlagged(); got != tt.want {
			t.Errorf("LicenseRisk(%q).IsFlagged() = %v, want %v", tt.risk, got, tt.want)
		}
	}
}

func TestClassifyLicense_CustomProprietaryText(t *testing.T) {
	// Custom commercial licenses with full license text should be detected as proprietary
	// based on keyword heuristics. Text must be > 200 chars to trigger heuristics.

	tests := []struct {
		name    string
		license string
		want    LicenseRisk
	}{
		{
			name:    "Bytewax-style commercial license with subscription fees",
			license: "The terms and conditions of this Software License Agreement govern your use of the accompanying software. Your download, installation or use of the Software constitutes acceptance. Subject to all terms and conditions of this License Agreement and your timely payment of all subscription fees due hereunder, we grant you a limited, nonexclusive, nontransferable license. You may not redistribute or sublicense the Software without express written permission.",
			want:    LicenseRiskProprietary,
		},
		{
			name:    "All rights reserved notice",
			license: "Copyright (c) 2024 Example Corp. All rights reserved. This software is provided under a commercial license agreement. Unauthorized copying, modification, or distribution of this software is strictly prohibited. Contact sales@example.com for licensing information and pricing details for enterprise use cases.",
			want:    LicenseRiskProprietary,
		},
		{
			name:    "Proprietary indicator with may not clauses",
			license: "This is proprietary software owned by Example Corporation. You may not copy, modify, or distribute this software without express written permission from the copyright holder. This license is granted for internal use only. Redistribution in any form is strictly prohibited without prior authorization.",
			want:    LicenseRiskProprietary,
		},
		{
			name:    "Short unknown license",
			license: "Custom License v1.0",
			want:    LicenseRiskUnknown, // Too short for heuristics, no known identifier
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLicense(tt.license)
			if got != tt.want {
				t.Errorf("ClassifyLicense() = %q, want %q (license len: %d)", got, tt.want, len(tt.license))
			}
		})
	}
}

func TestClassifyLicense_UnrecognizedTextIsUnknown(t *testing.T) {
	// Test that unrecognized license text is classified as unknown.
	// We intentionally don't try to guess from text heuristics because
	// it's too error-prone (e.g., "without warranty" appears in both
	// permissive AND proprietary licenses like Bytewax's commercial license).

	tests := []struct {
		name    string
		license string
		want    LicenseRisk
	}{
		{
			name: "MIT-style text without SPDX identifier",
			license: `Custom Permissive License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files, to deal in the Software
without restriction.`,
			want: LicenseRiskUnknown, // Can't reliably determine from text alone
		},
		{
			name: "Commercial license with permissive-looking boilerplate",
			license: `Software License Agreement

Subject to payment of subscription fees, we grant you a limited license.
THE SOFTWARE IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND.`,
			want: LicenseRiskUnknown, // Commercial but has permissive boilerplate
		},
		{
			name:    "Completely custom license",
			license: "This is a custom license with custom terms that do not match any known pattern.",
			want:    LicenseRiskUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLicense(tt.license)
			if got != tt.want {
				t.Errorf("ClassifyLicense() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLicenseRisk_IconColor(t *testing.T) {
	if c := LicenseRiskCopyleft.IconColor(); c == "" {
		t.Error("copyleft should have an icon color")
	}
	if c := LicenseRiskWeakCopyleft.IconColor(); c == "" {
		t.Error("weak-copyleft should have an icon color")
	}
	if c := LicenseRiskUnknown.IconColor(); c == "" {
		t.Error("unknown should have an icon color")
	}
	if c := LicenseRiskPermissive.IconColor(); c != "" {
		t.Errorf("permissive should have no icon color, got %q", c)
	}
}

func TestAnalyzeLicenses(t *testing.T) {
	g := dag.New(nil)
	// Root node at Row 0 (user's project - should be skipped)
	_ = g.AddNode(dag.Node{ID: "my-app", Row: 0, Meta: dag.Metadata{}})
	// Dependencies at Row 1+ (should be analyzed)
	_ = g.AddNode(dag.Node{ID: "express", Row: 1, Meta: dag.Metadata{"license": "MIT"}})
	_ = g.AddNode(dag.Node{ID: "lodash", Row: 1, Meta: dag.Metadata{"license": "MIT"}})
	_ = g.AddNode(dag.Node{ID: "readline", Row: 1, Meta: dag.Metadata{"license": "GPL-3.0"}})
	_ = g.AddNode(dag.Node{ID: "mystery", Row: 1, Meta: dag.Metadata{}}) // no license
	_ = g.AddNode(dag.Node{ID: "weak", Row: 1, Meta: dag.Metadata{"license": "LGPL-2.1"}})

	report := AnalyzeLicenses(g)

	if report.TotalDeps != 5 {
		t.Errorf("TotalDeps = %d, want 5", report.TotalDeps)
	}

	// Check MIT packages
	if len(report.Licenses["MIT"]) != 2 {
		t.Errorf("MIT packages = %d, want 2", len(report.Licenses["MIT"]))
	}

	// Check copyleft
	if len(report.Copyleft) != 1 || report.Copyleft[0] != "readline" {
		t.Errorf("Copyleft = %v, want [readline]", report.Copyleft)
	}

	// Check weak copyleft
	if len(report.WeakCopyleft) != 1 || report.WeakCopyleft[0] != "weak" {
		t.Errorf("WeakCopyleft = %v, want [weak]", report.WeakCopyleft)
	}

	// Check unknown
	if len(report.Unknown) != 1 || report.Unknown[0] != "mystery" {
		t.Errorf("Unknown = %v, want [mystery]", report.Unknown)
	}

	// Not compliant (has copyleft + unknown)
	if report.Compliant {
		t.Error("should not be compliant with copyleft and unknown licenses")
	}

	// Check that nodes were annotated
	n, _ := g.Node("readline")
	if risk, ok := n.Meta[MetaLicenseRisk].(string); !ok || risk != string(LicenseRiskCopyleft) {
		t.Errorf("readline license_risk = %q, want %q", risk, LicenseRiskCopyleft)
	}
	n, _ = g.Node("express")
	if risk, ok := n.Meta[MetaLicenseRisk].(string); !ok || risk != string(LicenseRiskPermissive) {
		t.Errorf("express license_risk = %q, want %q", risk, LicenseRiskPermissive)
	}
}

func TestAnalyzeLicenses_Compliant(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 0, Meta: dag.Metadata{}})
	_ = g.AddNode(dag.Node{ID: "a", Row: 1, Meta: dag.Metadata{"license": "MIT"}})
	_ = g.AddNode(dag.Node{ID: "b", Row: 1, Meta: dag.Metadata{"license": "Apache-2.0"}})
	_ = g.AddNode(dag.Node{ID: "c", Row: 1, Meta: dag.Metadata{"license": "BSD-3-Clause"}})

	report := AnalyzeLicenses(g)
	if !report.Compliant {
		t.Error("all-permissive graph should be compliant")
	}
}

func TestAnalyzeLicenses_ProprietaryFromText(t *testing.T) {
	// Test that a package with an unrecognized license identifier but proprietary
	// license text gets classified as proprietary (bytewax-influxdb case)
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 0, Meta: dag.Metadata{}})
	_ = g.AddNode(dag.Node{ID: "bytewax-influxdb", Row: 1, Meta: dag.Metadata{
		"license": "# Software License Agreement", // Unrecognized short identifier
		// Full proprietary license text (>200 chars to trigger heuristics)
		"license_text": `Subject to all terms and conditions of this License Agreement and your timely payment
of all subscription fees due hereunder, Bytewax hereby grants you a limited, nonexclusive,
nontransferable license. You may not copy, distribute, rent, lease, lend, sublicense or
transfer the Software. All rights reserved.`,
	}})

	report := AnalyzeLicenses(g)

	// Should be classified as proprietary, not unknown
	if len(report.Proprietary) != 1 || report.Proprietary[0] != "bytewax-influxdb" {
		t.Errorf("Proprietary = %v, want [bytewax-influxdb]", report.Proprietary)
	}
	if len(report.Unknown) != 0 {
		t.Errorf("Unknown = %v, want []", report.Unknown)
	}

	// Check the node was annotated correctly
	n, _ := g.Node("bytewax-influxdb")
	if risk, ok := n.Meta[MetaLicenseRisk].(string); !ok || risk != string(LicenseRiskProprietary) {
		t.Errorf("bytewax-influxdb license_risk = %q, want %q", risk, LicenseRiskProprietary)
	}
}

func TestAnalyzeLicenses_NilDAG(t *testing.T) {
	report := AnalyzeLicenses(nil)
	if report == nil {
		t.Fatal("should return non-nil report for nil DAG")
	}
	if !report.Compliant {
		t.Error("nil DAG should be compliant")
	}
}

func TestAnalyzeLicenses_SkipsSyntheticNodes(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "root", Row: 0, Meta: dag.Metadata{}})        // Root node - skipped
	_ = g.AddNode(dag.Node{ID: "__project__", Row: 1, Meta: dag.Metadata{}}) // __project__ marker - skipped
	_ = g.AddNode(dag.Node{ID: "a", Row: 1, Meta: dag.Metadata{"license": "MIT"}})

	report := AnalyzeLicenses(g)
	if report.TotalDeps != 1 {
		t.Errorf("TotalDeps = %d, want 1 (should skip root and __project__)", report.TotalDeps)
	}
}

func TestAnalyzeLicenses_SkipsRootNodes(t *testing.T) {
	g := dag.New(nil)
	// Multiple roots at Row 0 (e.g., monorepo with multiple packages)
	_ = g.AddNode(dag.Node{ID: "app-a", Row: 0, Meta: dag.Metadata{"license": "AGPL-3.0"}})
	_ = g.AddNode(dag.Node{ID: "app-b", Row: 0, Meta: dag.Metadata{}}) // No license
	// Dependencies at Row 1
	_ = g.AddNode(dag.Node{ID: "dep-1", Row: 1, Meta: dag.Metadata{"license": "MIT"}})
	_ = g.AddNode(dag.Node{ID: "dep-2", Row: 2, Meta: dag.Metadata{"license": "Apache-2.0"}})

	report := AnalyzeLicenses(g)

	// Should only count dependencies, not root nodes
	if report.TotalDeps != 2 {
		t.Errorf("TotalDeps = %d, want 2 (should skip Row 0 root nodes)", report.TotalDeps)
	}

	// Root node with AGPL should not be in copyleft list
	if len(report.Copyleft) != 0 {
		t.Errorf("Copyleft = %v, want [] (root nodes should be excluded)", report.Copyleft)
	}

	// Root node without license should not be in unknown list
	if len(report.Unknown) != 0 {
		t.Errorf("Unknown = %v, want [] (root nodes should be excluded)", report.Unknown)
	}

	// Should be compliant since only dependencies are MIT/Apache
	if !report.Compliant {
		t.Error("should be compliant when only dependencies have permissive licenses")
	}
}

func TestStripLicenseData(t *testing.T) {
	g := dag.New(nil)
	_ = g.AddNode(dag.Node{ID: "a", Row: 1, Meta: dag.Metadata{
		MetaLicenseRisk: string(LicenseRiskCopyleft),
		"license":       "GPL-3.0",
	}})

	StripLicenseData(g)

	n, _ := g.Node("a")
	if _, ok := n.Meta[MetaLicenseRisk]; ok {
		t.Error("StripLicenseData should remove license_risk from metadata")
	}
	// Should preserve other metadata
	if _, ok := n.Meta["license"]; !ok {
		t.Error("StripLicenseData should preserve 'license' metadata")
	}
}

func TestLicenseRiskFromString(t *testing.T) {
	tests := []struct {
		input string
		want  LicenseRisk
	}{
		{"permissive", LicenseRiskPermissive},
		{"copyleft", LicenseRiskCopyleft},
		{"weak-copyleft", LicenseRiskWeakCopyleft},
		{"unknown", LicenseRiskUnknown},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := LicenseRiskFromString(tt.input); got != tt.want {
			t.Errorf("LicenseRiskFromString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
