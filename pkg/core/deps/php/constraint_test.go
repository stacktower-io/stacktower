package php

import (
	"testing"
)

func TestComposerMatcher_ParseVersion(t *testing.T) {
	m := ComposerMatcher{}

	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{"1.2.3", "1.2.3", true},
		{"v1.2.3", "1.2.3", true},
		{"1.0.0", "1.0.0", true},
		{"0.1.0", "0.1.0", true},
		{"1.2", "1.2.0", true},
		{"1", "1.0.0", true},
		{"1.2.3-beta", "1.2.3", true},
		{"1.2.3@dev", "1.2.3", true},
		{"1.2.3-RC1", "1.2.3", true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v := m.ParseVersion(tt.input)
			if tt.valid {
				if v == nil {
					t.Errorf("ParseVersion(%q) = nil, want non-nil", tt.input)
					return
				}
				if got := v.String(); got != tt.want {
					t.Errorf("ParseVersion(%q).String() = %q, want %q", tt.input, got, tt.want)
				}
			} else {
				if v != nil {
					t.Errorf("ParseVersion(%q) = %v, want nil", tt.input, v)
				}
			}
		})
	}
}

func TestComposerMatcher_ParseConstraint(t *testing.T) {
	m := ComposerMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Exact match
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.0.1", false},
		{"=1.0.0", "1.0.0", true},
		{"=1.0.0", "1.0.1", false},

		// Caret (^)
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "2.0.0", false},
		{"^0.2.3", "0.2.5", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},

		// Tilde (~)
		{"~1.2.3", "1.2.3", true},
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.9.0", true},
		{"~1.2", "2.0.0", false},

		// Range operators
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "2.0.0", true},
		{">=1.0.0", "0.9.0", false},
		{">1.0.0", "1.0.0", false},
		{">1.0.0", "1.0.1", true},
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"<=2.0.0", "2.0.0", true},
		{"!=1.0.0", "1.0.0", false},
		{"!=1.0.0", "1.0.1", true},

		// Combined (space/comma = AND)
		{">=1.0.0 <2.0.0", "1.5.0", true},
		{">=1.0.0 <2.0.0", "2.0.0", false},
		{">=1.0.0,<2.0.0", "1.5.0", true},
		{">=1.0.0,<2.0.0", "2.0.0", false},

		// Wildcard ranges
		{"1.*", "1.0.0", true},
		{"1.*", "1.9.9", true},
		{"1.*", "2.0.0", false},
		{"1.2.*", "1.2.0", true},
		{"1.2.*", "1.2.9", true},
		{"1.2.*", "1.3.0", false},
		{"1.x", "1.0.0", true},
		{"1.x", "2.0.0", false},

		// Hyphen range
		{"1.0.0 - 2.0.0", "1.5.0", true},
		{"1.0.0 - 2.0.0", "2.0.0", true},
		{"1.0.0 - 2.0.0", "2.0.1", false},

		// OR (||)
		{"^1.0.0 || ^2.0.0", "1.5.0", true},
		{"^1.0.0 || ^2.0.0", "2.5.0", true},
		{"^1.0.0 || ^2.0.0", "3.0.0", false},

		// Wildcard
		{"*", "99.99.99", true},
	}

	for _, tt := range tests {
		t.Run(tt.constraint+"@"+tt.version, func(t *testing.T) {
			cond := m.ParseConstraint(tt.constraint)
			if cond == nil {
				t.Fatalf("ParseConstraint(%q) = nil", tt.constraint)
			}

			v := m.ParseVersion(tt.version)
			if v == nil {
				t.Fatalf("ParseVersion(%q) = nil", tt.version)
			}

			got := cond.Satisfies(v)
			if got != tt.matches {
				t.Errorf("Constraint %q, version %q: got Match=%v, want %v", tt.constraint, tt.version, got, tt.matches)
			}
		})
	}
}

func TestParseComposerVersion(t *testing.T) {
	tests := []struct {
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantStab  string
		wantValid bool
	}{
		{"1.2.3", 1, 2, 3, "", true},
		{"v1.2.3", 1, 2, 3, "", true},
		{"1.2", 1, 2, 0, "", true},
		{"1", 1, 0, 0, "", true},
		{"1.2.3-beta", 1, 2, 3, "beta", true},
		{"1.2.3@dev", 1, 2, 3, "dev", true},
		{"1.2.3-RC1", 1, 2, 3, "rc", true},
		{"1.0.0-alpha", 1, 0, 0, "alpha", true},
		{"invalid", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cv := parseComposerVersion(tt.input)
			if cv.valid != tt.wantValid {
				t.Errorf("parseComposerVersion(%q).valid = %v, want %v", tt.input, cv.valid, tt.wantValid)
			}
			if !tt.wantValid {
				return
			}
			if cv.major != tt.wantMajor {
				t.Errorf("parseComposerVersion(%q).major = %d, want %d", tt.input, cv.major, tt.wantMajor)
			}
			if cv.minor != tt.wantMinor {
				t.Errorf("parseComposerVersion(%q).minor = %d, want %d", tt.input, cv.minor, tt.wantMinor)
			}
			if cv.patch != tt.wantPatch {
				t.Errorf("parseComposerVersion(%q).patch = %d, want %d", tt.input, cv.patch, tt.wantPatch)
			}
			if cv.stability != tt.wantStab {
				t.Errorf("parseComposerVersion(%q).stability = %q, want %q", tt.input, cv.stability, tt.wantStab)
			}
		})
	}
}

func TestComposerSingleConstraintToRange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Caret
		{"^1.2.3", ">=1.2.3, <2.0.0"},
		{"^0.2.3", ">=0.2.3, <0.3.0"},
		{"^0.0.3", ">=0.0.3, <0.0.4"},

		// Tilde
		{"~1.2.3", ">=1.2.3, <1.3.0"},
		{"~1.2", ">=1.2.0, <2.0.0"},

		// Operators
		{">=1.0.0", ">=1.0.0"},
		{">1.0.0", ">1.0.0"},
		{"<2.0.0", "<2.0.0"},
		{"<=2.0.0", "<=2.0.0"},
		{"!=1.0.0", "!=1.0.0"},
		{"=1.0.0", "==1.0.0"},
		{"1.0.0", "==1.0.0"},

		// Wildcard ranges
		{"1.*", ">=1.0.0, <2.0.0"},
		{"1.2.*", ">=1.2.0, <1.3.0"},
		{"1.x", ">=1.0.0, <2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := composerSingleConstraintToRange(tt.input)
			if got != tt.want {
				t.Errorf("composerSingleConstraintToRange(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
