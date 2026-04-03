package python

import (
	"testing"

	pubgrub "github.com/contriboss/pubgrub-go"
)

func TestPEP440Matcher_BestMatch(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		candidates []string
		want       string
	}{
		{
			name:       "empty constraint returns empty",
			constraint: "",
			candidates: []string{"1.0.0", "2.0.0"},
			want:       "",
		},
		{
			name:       "empty candidates returns empty",
			constraint: ">=1.0",
			candidates: []string{},
			want:       "",
		},
		{
			name:       "greater than or equal",
			constraint: ">=1.0",
			candidates: []string{"0.9.0", "1.0.0", "1.5.0", "2.0.0"},
			want:       "2.0.0",
		},
		{
			name:       "less than",
			constraint: "<2.0",
			candidates: []string{"1.0.0", "1.5.0", "2.0.0", "2.5.0"},
			want:       "1.5.0",
		},
		{
			name:       "range constraint",
			constraint: ">=1.0,<2.0",
			candidates: []string{"0.5.0", "1.0.0", "1.9.0", "2.0.0", "2.5.0"},
			want:       "1.9.0",
		},
		{
			name:       "exact version",
			constraint: "==1.5.0",
			candidates: []string{"1.0.0", "1.5.0", "2.0.0"},
			want:       "1.5.0",
		},
		{
			name:       "not equal",
			constraint: "!=1.5.0",
			candidates: []string{"1.0.0", "1.5.0", "2.0.0"},
			want:       "2.0.0",
		},
		{
			name:       "compatible release with patch",
			constraint: "~=1.4.2",
			candidates: []string{"1.4.0", "1.4.2", "1.4.5", "1.5.0", "2.0.0"},
			want:       "1.4.5",
		},
		{
			name:       "compatible release without patch",
			constraint: "~=1.4",
			candidates: []string{"1.0.0", "1.4.0", "1.5.0", "1.9.0", "2.0.0"},
			want:       "1.9.0",
		},
		{
			name:       "no matching version",
			constraint: ">=3.0",
			candidates: []string{"1.0.0", "2.0.0"},
			want:       "",
		},
		{
			name:       "skips pre-release versions",
			constraint: ">=1.0",
			candidates: []string{"1.0.0", "2.0.0a1", "2.0.0b1", "2.0.0rc1", "1.5.0"},
			want:       "1.5.0",
		},
		{
			name:       "real world httpx constraint",
			constraint: ">=0.23.0,<1",
			candidates: []string{"0.20.0", "0.23.0", "0.24.0", "0.27.0", "1.0.0", "1.0.dev1"},
			want:       "0.27.0",
		},
		{
			name:       "real world anyio constraint",
			constraint: ">=3.5.0,<5",
			candidates: []string{"3.0.0", "3.5.0", "4.0.0", "4.7.0", "5.0.0"},
			want:       "4.7.0",
		},
		{
			name:       "handles spaces in constraint",
			constraint: ">= 1.0 , < 2.0",
			candidates: []string{"0.9.0", "1.0.0", "1.5.0", "2.0.0"},
			want:       "1.5.0",
		},
	}

	matcher := PEP440Matcher{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matcher.BestMatch(tt.constraint, tt.candidates)
			if got != tt.want {
				t.Errorf("BestMatch(%q, %v) = %q, want %q", tt.constraint, tt.candidates, got, tt.want)
			}
		})
	}
}

func TestPEP440Matcher_ParseVersion_PreservesOriginalString(t *testing.T) {
	m := PEP440Matcher{}
	cases := []struct {
		input string
	}{
		{"5.0.0a2"},
		{"4.1.0"},
		{"1.0.0rc1"},
		{"2.0.0b3"},
		{"3.0.0.dev1"},
	}
	for _, tc := range cases {
		v := m.ParseVersion(tc.input)
		if v == nil {
			t.Errorf("ParseVersion(%q) = nil", tc.input)
			continue
		}
		if v.String() != tc.input {
			t.Errorf("ParseVersion(%q).String() = %q, want %q", tc.input, v.String(), tc.input)
		}
	}
}

func TestPEP440Matcher_ParseVersion_PreReleaseOrdersBeforeStable(t *testing.T) {
	m := PEP440Matcher{}
	pre := m.ParseVersion("5.0.0a2")
	stable := m.ParseVersion("5.0.0")
	if pre == nil || stable == nil {
		t.Fatal("unexpected nil version")
	}
	if pre.Sort(stable) >= 0 {
		t.Errorf("expected 5.0.0a2 < 5.0.0 (Sort returned %d)", pre.Sort(stable))
	}
	if stable.Sort(pre) <= 0 {
		t.Errorf("expected 5.0.0 > 5.0.0a2 (Sort returned %d)", stable.Sort(pre))
	}
}

func TestPEP440Matcher_ParseConstraint_ExactVersionDoesNotMatchPreRelease(t *testing.T) {
	m := PEP440Matcher{}
	// ==5.0.0 must NOT satisfy 5.0.0a2
	cond := m.ParseConstraint("==5.0.0")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	pre := m.ParseVersion("5.0.0a2")
	stable := m.ParseVersion("5.0.0")
	if cond.Satisfies(pre) {
		t.Error("==5.0.0 should NOT satisfy 5.0.0a2")
	}
	if !cond.Satisfies(stable) {
		t.Error("==5.0.0 should satisfy 5.0.0")
	}
}

func TestPEP440Matcher_ParseConstraint_RangeIncludesPreReleaseWhenWithinBounds(t *testing.T) {
	m := PEP440Matcher{}
	// >=4.0.0 should include both 4.1.0 and 5.0.0a2
	cond := m.ParseConstraint(">=4.0.0")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	cases := []struct {
		version string
		want    bool
	}{
		{"3.9.0", false},
		{"4.0.0", true},
		{"4.1.0", true},
		{"5.0.0a2", true},
		{"5.0.0", true},
	}
	for _, tc := range cases {
		v := m.ParseVersion(tc.version)
		if got := cond.Satisfies(v); got != tc.want {
			t.Errorf(">=4.0.0 Satisfies(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestPEP440Matcher_ParseConstraint_CompatibleRelease(t *testing.T) {
	m := PEP440Matcher{}
	cond := m.ParseConstraint("~=1.4.2")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	cases := []struct {
		version string
		want    bool
	}{
		{"1.4.1", false},
		{"1.4.2", true},
		{"1.4.9", true},
		{"1.5.0", false},
	}
	for _, tc := range cases {
		v := m.ParseVersion(tc.version)
		if got := cond.Satisfies(v); got != tc.want {
			t.Errorf("~=1.4.2 Satisfies(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestPEP440Version_SortWithNonPEP440Version(t *testing.T) {
	m := PEP440Matcher{}
	v := m.ParseVersion("2.0.0")
	other := pubgrub.SimpleVersion("2.0.0")
	// Should not panic; exact value depends on implementation but 2.0.0 == 2.0.0
	_ = v.Sort(other)
}

func TestPEP440Matcher_ParseConstraint_LessThanExcludesPrereleaseOfBound(t *testing.T) {
	m := PEP440Matcher{}
	// <1.0.0 should exclude prereleases of 1.0.0 (like 1.0.0rc1)
	// but include stable versions below (like 0.52.1)
	cond := m.ParseConstraint("<1.0.0")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	cases := []struct {
		version string
		want    bool
	}{
		{"0.52.1", true},    // stable version below 1.0 - included
		{"0.99.0", true},    // stable version below 1.0 - included
		{"1.0.0rc1", false}, // prerelease OF 1.0.0 - excluded
		{"1.0.0a1", false},  // prerelease OF 1.0.0 - excluded
		{"1.0.0", false},    // the bound itself - excluded
		{"1.0.1", false},    // above the bound - excluded
	}
	for _, tc := range cases {
		v := m.ParseVersion(tc.version)
		if got := cond.Satisfies(v); got != tc.want {
			t.Errorf("<1.0.0 Satisfies(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestPEP440Matcher_ParseConstraint_LessThanWithPrereleaseBound(t *testing.T) {
	m := PEP440Matcher{}
	// <1.0.0rc1 with a prerelease bound.
	// Note: Current implementation treats all prereleases of the same version as
	// equivalent (doesn't distinguish dev < alpha < beta < rc). This is a known
	// limitation. Stable versions below work correctly.
	cond := m.ParseConstraint("<1.0.0rc1")
	if cond == nil {
		t.Fatal("ParseConstraint returned nil")
	}
	cases := []struct {
		version string
		want    bool
	}{
		{"0.99.0", true},    // stable below - included
		{"1.0.0a1", false},  // same-version prerelease treated as equal (limitation)
		{"1.0.0b1", false},  // same-version prerelease treated as equal (limitation)
		{"1.0.0rc1", false}, // the bound itself - excluded
		{"1.0.0", false},    // stable 1.0.0 - excluded (above rc1)
	}
	for _, tc := range cases {
		v := m.ParseVersion(tc.version)
		if got := cond.Satisfies(v); got != tc.want {
			t.Errorf("<1.0.0rc1 Satisfies(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantPre   bool
		wantValid bool
	}{
		{"1.0.0", 1, 0, 0, false, true},
		{"2.31.0", 2, 31, 0, false, true},
		{"1.4", 1, 4, 0, false, true},
		{"3", 3, 0, 0, false, true},
		{"1.0.0a1", 1, 0, 0, true, true},
		{"2.0.0beta1", 2, 0, 0, true, true},
		{"1.0.0rc1", 1, 0, 0, true, true},
		{"1.0.0.dev1", 1, 0, 0, true, true},
		{"v1.0.0", 1, 0, 0, false, true},
		{"invalid", 0, 0, 0, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pv := parseVersion(tt.input)
			if pv.valid != tt.wantValid {
				t.Errorf("valid = %v, want %v", pv.valid, tt.wantValid)
			}
			if pv.valid {
				if pv.major != tt.wantMajor {
					t.Errorf("major = %d, want %d", pv.major, tt.wantMajor)
				}
				if pv.minor != tt.wantMinor {
					t.Errorf("minor = %d, want %d", pv.minor, tt.wantMinor)
				}
				if pv.patch != tt.wantPatch {
					t.Errorf("patch = %d, want %d", pv.patch, tt.wantPatch)
				}
				if pv.prerelease != tt.wantPre {
					t.Errorf("prerelease = %v, want %v", pv.prerelease, tt.wantPre)
				}
			}
		})
	}
}
