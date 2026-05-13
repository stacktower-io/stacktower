package rust

import (
	"testing"
)

func TestCargoMatcher_ParseVersion(t *testing.T) {
	m := CargoMatcher{}

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
		{"1.2.3-beta.1", "1.2.3-beta.1", true},
		{"0.24.0+spec-1.1.0", "0.24.0+spec-1.1.0", true},
		{"1.2.3-alpha-1", "1.2.3-alpha-1", true},
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

func TestCargoMatcher_ParseConstraint(t *testing.T) {
	m := CargoMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Caret (^) - default operator
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "2.0.0", false},
		{"1.2.3", "1.2.3", true},  // default is caret
		{"1.2.3", "1.9.9", true},  // default is caret
		{"1.2.3", "2.0.0", false}, // default is caret

		// Caret with 0.x.y
		{"^0.2.3", "0.2.3", true},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},

		// Caret with 0.0.x
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
		{"=1.0.0", "1.0.0", true},
		{"=1.0.0", "1.0.1", false},

		// Combined (comma = AND)
		{">=1.0.0, <2.0.0", "1.5.0", true},
		{">=1.0.0, <2.0.0", "2.0.0", false},
		{">=0.24.0, <0.25.0", "0.24.0+spec-1.1.0", true},

		// Wildcard ranges
		{"1.*", "1.0.0", true},
		{"1.*", "1.9.9", true},
		{"1.*", "2.0.0", false},
		{"1.2.*", "1.2.0", true},
		{"1.2.*", "1.2.9", true},
		{"1.2.*", "1.3.0", false},

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

func TestParseCargoVersion(t *testing.T) {
	tests := []struct {
		input      string
		wantMajor  int
		wantMinor  int
		wantPatch  int
		wantPrerel string
		wantValid  bool
	}{
		{"1.2.3", 1, 2, 3, "", true},
		{"v1.2.3", 1, 2, 3, "", true},
		{"1.2", 1, 2, 0, "", true},
		{"1", 1, 0, 0, "", true},
		{"1.2.3-beta.1", 1, 2, 3, "beta.1", true},
		{"1.2.3-alpha-1", 1, 2, 3, "alpha-1", true},
		{"0.24.0+spec-1.1.0", 0, 24, 0, "", true},
		{"0.0.0", 0, 0, 0, "", true},
		{"invalid", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cv := parseCargoVersion(tt.input)
			if cv.valid != tt.wantValid {
				t.Errorf("parseCargoVersion(%q).valid = %v, want %v", tt.input, cv.valid, tt.wantValid)
			}
			if !tt.wantValid {
				return
			}
			if cv.major != tt.wantMajor {
				t.Errorf("parseCargoVersion(%q).major = %d, want %d", tt.input, cv.major, tt.wantMajor)
			}
			if cv.minor != tt.wantMinor {
				t.Errorf("parseCargoVersion(%q).minor = %d, want %d", tt.input, cv.minor, tt.wantMinor)
			}
			if cv.patch != tt.wantPatch {
				t.Errorf("parseCargoVersion(%q).patch = %d, want %d", tt.input, cv.patch, tt.wantPatch)
			}
			if cv.prerelease != tt.wantPrerel {
				t.Errorf("parseCargoVersion(%q).prerelease = %q, want %q", tt.input, cv.prerelease, tt.wantPrerel)
			}
		})
	}
}

func TestCargoMajorBucket(t *testing.T) {
	tests := []struct {
		major, minor, patch int
		want                string
	}{
		{1, 0, 0, "1"},
		{2, 5, 3, "2"},
		{0, 2, 3, "0.2"},
		{0, 9, 0, "0.9"},
		{0, 0, 3, "0.0.3"},
		{0, 0, 0, "0.0.0"},
	}
	for _, tt := range tests {
		got := cargoMajorBucket(tt.major, tt.minor, tt.patch)
		if got != tt.want {
			t.Errorf("cargoMajorBucket(%d,%d,%d) = %q, want %q", tt.major, tt.minor, tt.patch, got, tt.want)
		}
	}
}

func TestCargoCompatBucket(t *testing.T) {
	tests := []struct {
		constraint string
		want       string
	}{
		{"^1.2.3", "1"},
		{"^2.0.0", "2"},
		{"^0.2.3", "0.2"},
		{"^0.0.3", "0.0.3"},
		{"1.2.3", "1"},
		{"~1.2.3", "1"},
		{">=1.0.0, <2.0.0", "1"},
		{">=0.8.0, <0.9.0", "0.8"},
		{"=1.5.0", "1"},
		{"*", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got := cargoCompatBucket(tt.constraint)
			if got != tt.want {
				t.Errorf("cargoCompatBucket(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestCargoNamespacing(t *testing.T) {
	tests := []struct {
		name, bucket         string
		wantNs               string
		wantReal, wantBucket string
	}{
		{"thiserror", "2", "thiserror/2", "thiserror", "2"},
		{"thiserror", "1", "thiserror/1", "thiserror", "1"},
		{"serde", "1", "serde/1", "serde", "1"},
		{"rand", "0.8", "rand/0.8", "rand", "0.8"},
		{"tiny", "0.0.3", "tiny/0.0.3", "tiny", "0.0.3"},
		{"foo", "", "foo", "foo", ""},
	}
	for _, tt := range tests {
		t.Run(tt.wantNs, func(t *testing.T) {
			ns := cargoNamespacedName(tt.name, tt.bucket)
			if ns != tt.wantNs {
				t.Errorf("cargoNamespacedName(%q, %q) = %q, want %q", tt.name, tt.bucket, ns, tt.wantNs)
			}
			real, bucket := cargoSplitNamespacedName(ns)
			if real != tt.wantReal || bucket != tt.wantBucket {
				t.Errorf("cargoSplitNamespacedName(%q) = (%q, %q), want (%q, %q)", ns, real, bucket, tt.wantReal, tt.wantBucket)
			}
		})
	}
}

func TestCargoSingleConstraintToRange(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Caret (default)
		{"^1.2.3", ">=1.2.3, <2.0.0"},
		{"1.2.3", ">=1.2.3, <2.0.0"},
		{"^0.2.3", ">=0.2.3, <0.3.0"},
		{"^0.0.3", ">=0.0.3, <0.0.4"},

		// Tilde
		{"~1.2.3", ">=1.2.3, <1.3.0"},
		{"~1.2", ">=1.2.0, <1.3.0"},
		{"~1", ">=1.0.0, <2.0.0"},

		// Operators
		{">=1.0.0", ">=1.0.0"},
		{">1.0.0", ">1.0.0"},
		{"<2.0.0", "<2.0.0"},
		{"<=2.0.0", "<=2.0.0"},
		{"=1.0.0", "==1.0.0"},

		// Wildcard ranges
		{"1.*", ">=1.0.0, <2.0.0"},
		{"1.2.*", ">=1.2.0, <1.3.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cargoSingleConstraintToRange(tt.input)
			if got != tt.want {
				t.Errorf("cargoSingleConstraintToRange(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
