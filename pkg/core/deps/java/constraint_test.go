package java

import (
	"testing"
)

func TestMavenMatcher_ParseVersion(t *testing.T) {
	m := MavenMatcher{}

	tests := []struct {
		input string
		want  string
		valid bool
	}{
		// SimpleVersion preserves the original string to ensure cache consistency
		// for versions with qualifiers (jre, android, SNAPSHOT, etc.)
		{"1.2.3", "1.2.3", true},
		{"1.0.0", "1.0.0", true},
		{"1.0", "1.0", true},                       // preserved as-is
		{"1", "1", true},                           // preserved as-is
		{"32.1.3-jre", "32.1.3-jre", true},         // qualifier preserved
		{"1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT", true}, // qualifier preserved
		{"1.2.3.Final", "1.2.3.Final", true},       // qualifier preserved
		{"9999.0-empty-to-avoid-conflict-with-guava", "9999.0-empty-to-avoid-conflict-with-guava", true},
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

func TestMavenMatcher_ParseConstraint(t *testing.T) {
	m := MavenMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Plain version (treated as exact match to preserve qualifiers)
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.0.1", false}, // exact match, not >=
		{"1.0.0", "2.0.0", false}, // exact match, not >=
		{"1.0.0", "0.9.0", false},

		// Plain version with qualifier (must match exactly)
		{"32.1.3-jre", "32.1.3-jre", true},
		{"32.1.3-jre", "32.1.3", false}, // different version string
		{"32.1.3-jre", "32.1.3-android", false},
		{"9999.0-empty-to-avoid-conflict-with-guava", "9999.0-empty-to-avoid-conflict-with-guava", true},

		// Exact match with brackets
		{"[1.0.0]", "1.0.0", true},
		{"[1.0.0]", "1.0.1", false},

		// Range: [1.0,2.0) - inclusive lower, exclusive upper
		{"[1.0,2.0)", "1.0.0", true},
		{"[1.0,2.0)", "1.5.0", true},
		{"[1.0,2.0)", "2.0.0", false},

		// Range: (1.0,2.0] - exclusive lower, inclusive upper
		{"(1.0,2.0]", "1.0.0", false},
		{"(1.0,2.0]", "1.0.1", true},
		{"(1.0,2.0]", "2.0.0", true},

		// Range: [1.0,) - at least 1.0
		{"[1.0,)", "1.0.0", true},
		{"[1.0,)", "2.0.0", true},
		{"[1.0,)", "0.9.0", false},

		// Range: (,2.0] - up to and including 2.0
		{"(,2.0]", "1.0.0", true},
		{"(,2.0]", "2.0.0", true},
		{"(,2.0]", "2.0.1", false},

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

func TestParseMavenVersion(t *testing.T) {
	tests := []struct {
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantQual  string
		wantValid bool
	}{
		{"1.2.3", 1, 2, 3, "", true},
		{"1.2", 1, 2, 0, "", true},
		{"1", 1, 0, 0, "", true},
		{"32.1.3-jre", 32, 1, 3, "jre", true},
		{"1.2.3-SNAPSHOT", 1, 2, 3, "SNAPSHOT", true},
		{"1.2.3.Final", 1, 2, 3, "Final", true},
		{"1.0.0-alpha-1", 1, 0, 0, "alpha-1", true},
		{"invalid", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mv := parseMavenVersion(tt.input)
			if mv.valid != tt.wantValid {
				t.Errorf("parseMavenVersion(%q).valid = %v, want %v", tt.input, mv.valid, tt.wantValid)
			}
			if !tt.wantValid {
				return
			}
			if mv.major != tt.wantMajor {
				t.Errorf("parseMavenVersion(%q).major = %d, want %d", tt.input, mv.major, tt.wantMajor)
			}
			if mv.minor != tt.wantMinor {
				t.Errorf("parseMavenVersion(%q).minor = %d, want %d", tt.input, mv.minor, tt.wantMinor)
			}
			if mv.patch != tt.wantPatch {
				t.Errorf("parseMavenVersion(%q).patch = %d, want %d", tt.input, mv.patch, tt.wantPatch)
			}
			if mv.qualifier != tt.wantQual {
				t.Errorf("parseMavenVersion(%q).qualifier = %q, want %q", tt.input, mv.qualifier, tt.wantQual)
			}
		})
	}
}

func TestSplitMavenRanges(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"[1.0,2.0)", []string{"[1.0,2.0)"}},
		{"[1.0,2.0),[3.0,4.0)", []string{"[1.0,2.0)", "[3.0,4.0)"}},
		{"(,1.0],[2.0,)", []string{"(,1.0]", "[2.0,)"}},
		{"1.0", []string{"1.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitMavenRanges(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitMavenRanges(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitMavenRanges(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMavenSingleRangeToRange(t *testing.T) {
	// Note: mavenSingleRangeToRange is now only used for actual range expressions.
	// Plain versions and exact matches [1.0.0] are handled directly in ParseConstraint
	// using EqualsCondition to preserve version strings with qualifiers.
	tests := []struct {
		input string
		want  string
	}{
		{"[1.0,2.0)", ">=1.0.0, <2.0.0"},
		{"(1.0,2.0]", ">1.0.0, <=2.0.0"},
		{"[1.0,)", ">=1.0.0"},
		{"(,2.0]", "<=2.0.0"},
		// Plain versions and exact matches return "" since they're handled elsewhere
		{"1.0.0", ""},
		{"[1.0.0]", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mavenSingleRangeToRange(tt.input)
			if got != tt.want {
				t.Errorf("mavenSingleRangeToRange(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
