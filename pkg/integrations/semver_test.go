package integrations

import (
	"reflect"
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		want  SemanticVersion
	}{
		{"1.2.3", SemanticVersion{Original: "1.2.3", Major: 1, Minor: 2, Patch: 3, Valid: true}},
		{"v1.2.3", SemanticVersion{Original: "v1.2.3", Major: 1, Minor: 2, Patch: 3, Valid: true}},
		{"1.2", SemanticVersion{Original: "1.2", Major: 1, Minor: 2, Patch: 0, Valid: true}},
		{"1", SemanticVersion{Original: "1", Major: 1, Minor: 0, Patch: 0, Valid: true}},
		{"1.2.3-alpha", SemanticVersion{Original: "1.2.3-alpha", Major: 1, Minor: 2, Patch: 3, Prerelease: "alpha", Valid: true}},
		{"1.2.3-beta.1", SemanticVersion{Original: "1.2.3-beta.1", Major: 1, Minor: 2, Patch: 3, Prerelease: "beta.1", Valid: true}},
		{"1.2.3+build", SemanticVersion{Original: "1.2.3+build", Major: 1, Minor: 2, Patch: 3, Build: "build", Valid: true}},
		{"1.2.3-rc.1+build", SemanticVersion{Original: "1.2.3-rc.1+build", Major: 1, Minor: 2, Patch: 3, Prerelease: "rc.1", Build: "build", Valid: true}},
		{"invalid", SemanticVersion{Original: "invalid", Valid: false}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseSemver(tt.input)
			if got.Original != tt.want.Original || got.Major != tt.want.Major ||
				got.Minor != tt.want.Minor || got.Patch != tt.want.Patch ||
				got.Prerelease != tt.want.Prerelease || got.Build != tt.want.Build ||
				got.Valid != tt.want.Valid {
				t.Errorf("ParseSemver(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSemanticVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1 (a < b), 0 (a == b), 1 (a > b)
	}{
		// Basic comparisons
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.1", "1.0.0", 1},

		// Prerelease comparisons
		{"1.0.0", "1.0.0-alpha", 1},  // stable > prerelease
		{"1.0.0-alpha", "1.0.0", -1}, // prerelease < stable
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.2", -1},
		{"1.0.0-alpha.2", "1.0.0-alpha.10", -1}, // numeric comparison
		{"1.0.0-1", "1.0.0-2", -1},
		{"1.0.0-alpha", "1.0.0-1", 1}, // alphanumeric > numeric

		// With v prefix
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},

		// Invalid versions
		{"invalid", "1.0.0", 1},  // invalid sorts to end
		{"1.0.0", "invalid", -1}, // valid before invalid
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a := ParseSemver(tt.a)
			b := ParseSemver(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("%q.Compare(%q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "basic semver",
			input: []string{"1.0.0", "2.0.0", "1.5.0", "1.0.1"},
			want:  []string{"1.0.0", "1.0.1", "1.5.0", "2.0.0"},
		},
		{
			name:  "with v prefix",
			input: []string{"v1.0.0", "v2.0.0", "v1.5.0"},
			want:  []string{"v1.0.0", "v1.5.0", "v2.0.0"},
		},
		{
			name:  "with prerelease",
			input: []string{"1.0.0", "1.0.0-alpha", "1.0.0-beta", "1.0.0-rc.1"},
			want:  []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0-rc.1", "1.0.0"},
		},
		{
			name:  "mixed npm style",
			input: []string{"4.17.21", "4.17.20", "4.18.0", "4.17.0"},
			want:  []string{"4.17.0", "4.17.20", "4.17.21", "4.18.0"},
		},
		{
			name:  "invalid versions at end",
			input: []string{"1.0.0", "invalid", "2.0.0", "notversion"},
			want:  []string{"1.0.0", "2.0.0", "invalid", "notversion"},
		},
		{
			name:  "go module versions",
			input: []string{"v0.1.0", "v0.2.0", "v1.0.0", "v0.10.0"},
			want:  []string{"v0.1.0", "v0.2.0", "v0.10.0", "v1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying test data
			input := make([]string, len(tt.input))
			copy(input, tt.input)

			SortVersions(input)

			if !reflect.DeepEqual(input, tt.want) {
				t.Errorf("SortVersions() = %v, want %v", input, tt.want)
			}
		})
	}
}

func TestSortVersionsDescending(t *testing.T) {
	input := []string{"1.0.0", "2.0.0", "1.5.0", "1.0.1"}
	want := []string{"2.0.0", "1.5.0", "1.0.1", "1.0.0"}

	SortVersionsDescending(input)

	if !reflect.DeepEqual(input, want) {
		t.Errorf("SortVersionsDescending() = %v, want %v", input, want)
	}
}

func TestLatestVersion(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"basic", []string{"1.0.0", "2.0.0", "1.5.0"}, "2.0.0"},
		{"with prerelease same major.minor", []string{"1.0.0", "1.0.0-alpha", "1.0.0-beta"}, "1.0.0"},
		{"prerelease lower version", []string{"1.0.0", "1.0.0-alpha", "1.1.0-beta"}, "1.1.0-beta"}, // 1.1 > 1.0 even as prerelease
		{"empty", []string{}, ""},
		{"single", []string{"1.0.0"}, "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LatestVersion(tt.input)
			if got != tt.want {
				t.Errorf("LatestVersion(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
