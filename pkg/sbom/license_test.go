package sbom

import "testing"

func TestIsSPDXExpression(t *testing.T) {
	valid := []string{
		"MIT",
		"Apache-2.0",
		"MIT OR Apache-2.0",
		"MIT AND Apache-2.0",
		"(MIT OR Apache-2.0) AND BSD-3-Clause",
		"GPL-2.0-only WITH Classpath-exception-2.0",
		"MIT OR (GPL-3.0-or-later AND BSD-2-Clause)",
	}
	for _, s := range valid {
		if !isSPDXExpression(s) {
			t.Errorf("isSPDXExpression(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"",
		"MIT OR",
		"OR MIT",
		"MIT Apache-2.0",
		"(MIT OR Apache-2.0",
		"MIT OR Apache-2.0)",
		"Copyright (c) 2024 The Authors. All rights reserved.",
		"See LICENSE file",
	}
	for _, s := range invalid {
		if isSPDXExpression(s) {
			t.Errorf("isSPDXExpression(%q) = true, want false", s)
		}
	}
}

func TestCDXLicenseFor(t *testing.T) {
	// Simple SPDX id -> license.id
	c := cdxLicenseFor("MIT")
	if c.License == nil || c.License.ID != "MIT" || c.Expression != "" {
		t.Errorf("MIT: %+v", c)
	}

	// Expression -> expression field
	c = cdxLicenseFor("MIT OR Apache-2.0")
	if c.Expression != "MIT OR Apache-2.0" || c.License != nil {
		t.Errorf("expression: %+v", c)
	}

	// Free text -> license.name
	c = cdxLicenseFor("Custom Proprietary License v2")
	if c.License == nil || c.License.Name != "Custom Proprietary License v2" || c.License.ID != "" {
		t.Errorf("free text: %+v", c)
	}
}
