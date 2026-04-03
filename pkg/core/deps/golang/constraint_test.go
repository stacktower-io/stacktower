package golang

import (
	"testing"
)

func TestGoModMatcher_ParseVersion(t *testing.T) {
	m := GoModMatcher{}

	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{"v1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.3", true},
		{"v1.0.0", "1.0.0", true},
		{"v0.1.0", "0.1.0", true},
		{"v1.2.3-beta.1", "1.2.3-beta.1", true},
		{"v0.0.0-20201130134442-10cb98267c6c", "0.0.0-20201130134442-10cb98267c6c", true}, // pseudo-version
		{"v1.2.3+incompatible", "1.2.3", true},
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

func TestGoModMatcher_ParseConstraint(t *testing.T) {
	m := GoModMatcher{}

	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// MVS: constraint means "at least this version"
		{"v1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v1.3.0", true},
		{"v1.2.3", "v2.0.0", true},
		{"v1.2.3", "v1.2.2", false},
		{"v1.2.3", "v1.1.0", false},

		// Version without v prefix
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.2.2", false},

		// Pseudo-versions act as lower bounds too
		{"v0.0.0-20201130134442-10cb98267c6c", "v0.0.0-20201130134442-10cb98267c6c", true},
		{"v0.0.0-20201130134442-10cb98267c6c", "v0.1.0", true}, // real release > pseudo-version

		// Beta/pre-release versions
		{"v1.0.0-beta.1", "v1.0.0-beta.1", true},
		{"v1.0.0-beta.1", "v1.0.0", true}, // stable > beta

		// "=" prefix (internal representation) - still uses MVS (>= lower bound)
		{"=v1.2.3", "v1.2.3", true},  // matches exact version
		{"=v1.2.3", "v1.2.4", true},  // MVS: higher versions satisfy >= constraint
		{"=v1.2.3", "v1.2.2", false}, // lower versions don't satisfy

		// "=" prefix with prerelease (e.g., deprecated modules)
		{"=v0.1.1-deprecated", "v0.1.1-deprecated", true},
		{"=v0.1.1-deprecated", "v0.1.0-deprecated", false}, // lower doesn't satisfy
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

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		input      string
		wantMajor  int
		wantMinor  int
		wantPatch  int
		wantPrerel string
		wantValid  bool
	}{
		{"v1.2.3", 1, 2, 3, "", true},
		{"1.2.3", 1, 2, 3, "", true},
		{"v1.0.0", 1, 0, 0, "", true},
		{"v0.0.0-20201130134442-10cb98267c6c", 0, 0, 0, "20201130134442-10cb98267c6c", true},
		{"v1.2.3-beta.1", 1, 2, 3, "beta.1", true},
		{"v1.2.3+incompatible", 1, 2, 3, "", true},
		{"invalid", 0, 0, 0, "", false},
		{"", 0, 0, 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gv := parseGoVersion(tt.input)
			if gv.valid != tt.wantValid {
				t.Errorf("parseGoVersion(%q).valid = %v, want %v", tt.input, gv.valid, tt.wantValid)
			}
			if !tt.wantValid {
				return
			}
			if gv.major != tt.wantMajor {
				t.Errorf("parseGoVersion(%q).major = %d, want %d", tt.input, gv.major, tt.wantMajor)
			}
			if gv.minor != tt.wantMinor {
				t.Errorf("parseGoVersion(%q).minor = %d, want %d", tt.input, gv.minor, tt.wantMinor)
			}
			if gv.patch != tt.wantPatch {
				t.Errorf("parseGoVersion(%q).patch = %d, want %d", tt.input, gv.patch, tt.wantPatch)
			}
			if gv.prerelease != tt.wantPrerel {
				t.Errorf("parseGoVersion(%q).prerelease = %q, want %q", tt.input, gv.prerelease, tt.wantPrerel)
			}
		})
	}
}

func TestGoModMatcher_HintedVersion(t *testing.T) {
	m := GoModMatcher{}

	tests := []struct {
		constraint string
		want       string
	}{
		// "=" prefix extracts the version for hinting
		{"=v1.2.3", "v1.2.3"},
		{"=v0.0.0-20231006140011-abc123", "v0.0.0-20231006140011-abc123"},
		{"=v0.1.1-deprecated", "v0.1.1-deprecated"},

		// No "=" prefix returns empty (no hinting needed)
		{"v1.2.3", ""},
		{">=1.0.0", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got := m.HintedVersion(tt.constraint)
			if got != tt.want {
				t.Errorf("HintedVersion(%q) = %q, want %q", tt.constraint, got, tt.want)
			}
		})
	}
}
