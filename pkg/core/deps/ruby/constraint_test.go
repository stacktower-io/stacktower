package ruby

import (
	"testing"
)

func TestGemMatcher_ParseVersion(t *testing.T) {
	m := GemMatcher{}

	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{"1.2.3", "1.2.3", true},
		{"1.0", "1.0.0", true},
		{"1", "1.0.0", true},
		{"1.2.3.4", "1.2.3", true}, // 4th segment ignored in semver
		{"0.1.0", "0.1.0", true},
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

func TestGemMatcher_ParseConstraint(t *testing.T) {
	m := GemMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Exact match
		{"= 1.0.0", "1.0.0", true},
		{"= 1.0.0", "1.0.1", false},

		// Pessimistic (~>)
		{"~> 1.2.3", "1.2.3", true},
		{"~> 1.2.3", "1.2.9", true},
		{"~> 1.2.3", "1.3.0", false},
		{"~> 1.2", "1.2.0", true},
		{"~> 1.2", "1.9.0", true},
		{"~> 1.2", "2.0.0", false},
		{"~> 1", "1.0.0", true},
		{"~> 1", "1.9.9", true},
		{"~> 1", "2.0.0", false},

		// Range operators
		{">= 1.0.0", "1.0.0", true},
		{">= 1.0.0", "2.0.0", true},
		{">= 1.0.0", "0.9.0", false},
		{"> 1.0.0", "1.0.0", false},
		{"> 1.0.0", "1.0.1", true},
		{"< 2.0.0", "1.9.9", true},
		{"< 2.0.0", "2.0.0", false},
		{"<= 2.0.0", "2.0.0", true},
		{"!= 1.0.0", "1.0.0", false},
		{"!= 1.0.0", "1.0.1", true},

		// Combined (comma = AND)
		{">= 1.0.0, < 2.0.0", "1.5.0", true},
		{">= 1.0.0, < 2.0.0", "2.0.0", false},
		{">= 1.0.0, < 2.0.0", "0.9.0", false},

		// Default wildcard
		{">= 0", "99.99.99", true},
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

func TestGemSingleConstraintToRange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"~> 1.2.3", ">=1.2.3, <1.3.0"},
		{"~> 1.2", ">=1.2.0, <2.0.0"},
		{"~> 1", ">=1.0.0, <2.0.0"},
		{">= 1.0.0", ">=1.0.0"},
		{"> 1.0.0", ">1.0.0"},
		{"< 2.0.0", "<2.0.0"},
		{"<= 2.0.0", "<=2.0.0"},
		{"= 1.2.3", "==1.2.3"},
		{"!= 1.0.0", "!=1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := gemSingleConstraintToRange(tt.input)
			if got != tt.want {
				t.Errorf("gemSingleConstraintToRange(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
