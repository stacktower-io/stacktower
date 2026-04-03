package javascript

import (
	"testing"
)

func TestSemverMatcher_ParseVersion(t *testing.T) {
	m := SemverMatcher{}

	tests := []struct {
		input string
		want  string // expected normalized output
		valid bool
	}{
		{"1.2.3", "1.2.3", true},
		{"v1.2.3", "1.2.3", true},
		{"1.0.0", "1.0.0", true},
		{"0.1.0", "0.1.0", true},
		{"1.2.3-beta.1", "1.2.3", true}, // prerelease stripped in semver
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

func TestSemverMatcher_ParseConstraint(t *testing.T) {
	m := SemverMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Exact match
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.0.1", false},

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

		// Range operators
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "2.0.0", true},
		{">=1.0.0", "0.9.0", false},
		{">1.0.0", "1.0.0", false},
		{">1.0.0", "1.0.1", true},
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"<=2.0.0", "2.0.0", true},

		// Combined (space = AND)
		{">=1.0.0 <2.0.0", "1.5.0", true},
		{">=1.0.0 <2.0.0", "2.0.0", false},

		// Spaced operators (npm allows spaces after operators)
		{">= 2.1.2 < 3", "2.5.0", true},
		{">= 2.1.2 < 3", "2.1.2", true},
		{">= 2.1.2 < 3", "3.0.0", false},
		{">= 2.1.2 < 3", "2.1.1", false},

		// X-range
		{"1.x", "1.0.0", true},
		{"1.x", "1.9.9", true},
		{"1.x", "2.0.0", false},
		{"1.2.x", "1.2.0", true},
		{"1.2.x", "1.3.0", false},

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

func TestConstraintToRange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"^1.2.3", ">=1.2.3, <2.0.0"},
		{"^0.2.3", ">=0.2.3, <0.3.0"},
		{"^0.0.3", ">=0.0.3, <0.0.4"},
		{"~1.2.3", ">=1.2.3, <1.3.0"},
		{">=1.0.0", ">=1.0.0"},
		{">1.0.0", ">1.0.0"},
		{"<2.0.0", "<2.0.0"},
		{"<=2.0.0", "<=2.0.0"},
		{"1.2.3", "==1.2.3"},
		{"=1.2.3", "==1.2.3"},
		{"1.x", ">=1.0.0, <2.0.0"},
		{"1.2.x", ">=1.2.0, <1.3.0"},
		{"*", "*"},
		{"1.0.0 - 2.0.0", ">=1.0.0, <=2.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := constraintToRange(tt.input)
			if got != tt.want {
				t.Errorf("constraintToRange(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
