package deps

import "testing"

func TestBuildAndParsePackageID(t *testing.T) {
	tests := []struct {
		name         string
		inputName    string
		inputVersion string
		inputCommit  string
		wantID       string
		wantName     string
		wantVersion  string
		wantCommit   string
	}{
		{
			name:         "simple package with version",
			inputName:    "requests",
			inputVersion: "2.31.0",
			wantID:       "requests@2.31.0",
			wantName:     "requests",
			wantVersion:  "2.31.0",
		},
		{
			name:        "simple package with commit",
			inputName:   "my-lib",
			inputCommit: "abc1234",
			wantID:      "my-lib@commit:abc1234",
			wantName:    "my-lib",
			wantCommit:  "abc1234",
		},
		{
			name:      "simple package name only",
			inputName: "requests",
			wantID:    "requests",
			wantName:  "requests",
		},
		{
			name:         "scoped npm package with version",
			inputName:    "@types/node",
			inputVersion: "20.1.0",
			wantID:       "@types/node@20.1.0",
			wantName:     "@types/node",
			wantVersion:  "20.1.0",
		},
		{
			name:      "scoped npm package name only",
			inputName: "@types/node",
			wantID:    "@types/node",
			wantName:  "@types/node",
		},
		{
			name:        "scoped npm package with commit",
			inputName:   "@scope/pkg",
			inputCommit: "deadbeef",
			wantID:      "@scope/pkg@commit:deadbeef",
			wantName:    "@scope/pkg",
			wantCommit:  "deadbeef",
		},
		{
			name:   "empty name returns empty",
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID := BuildPackageID(tt.inputName, tt.inputVersion, tt.inputCommit)
			if gotID != tt.wantID {
				t.Errorf("BuildPackageID(%q, %q, %q) = %q, want %q",
					tt.inputName, tt.inputVersion, tt.inputCommit, gotID, tt.wantID)
			}

			if gotID == "" {
				return
			}

			gotName, gotVersion, gotCommit := ParsePackageID(gotID)
			if gotName != tt.wantName {
				t.Errorf("ParsePackageID(%q) name = %q, want %q", gotID, gotName, tt.wantName)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("ParsePackageID(%q) version = %q, want %q", gotID, gotVersion, tt.wantVersion)
			}
			if gotCommit != tt.wantCommit {
				t.Errorf("ParsePackageID(%q) commit = %q, want %q", gotID, gotCommit, tt.wantCommit)
			}
		})
	}
}

func TestParsePackageID_EdgeCases(t *testing.T) {
	tests := []struct {
		id          string
		wantName    string
		wantVersion string
		wantCommit  string
	}{
		{"", "", "", ""},
		{"simple", "simple", "", ""},
		{"a@1", "a", "1", ""},
		{"@scope/pkg", "@scope/pkg", "", ""},
		{"@scope/pkg@1.2.3", "@scope/pkg", "1.2.3", ""},
		{"@scope/pkg@commit:abc", "@scope/pkg", "", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			name, version, commit := ParsePackageID(tt.id)
			if name != tt.wantName || version != tt.wantVersion || commit != tt.wantCommit {
				t.Errorf("ParsePackageID(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.id, name, version, commit, tt.wantName, tt.wantVersion, tt.wantCommit)
			}
		})
	}
}

func TestIsPrereleaseVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		// Standard prereleases
		{"1.0.0-alpha.1", true},
		{"2.0.0-beta.2", true},
		{"1.0.0-rc.1", true},
		{"1.0.0-dev", true},
		{"1.0.0-preview.1", true},
		{"3.0.0-canary.1", true},
		{"1.0.0-nightly.20230101", true},
		{"2.0.0-next.1", true},
		{"1.0.0-snapshot", true},

		// PEP 440
		{"2.13.0b1", true},
		{"1.0.0a1", true},

		// Maven milestones
		{"7.0.0-M6", true},
		{"3.0.0-M1", true},

		// Stable versions
		{"1.0.0", false},
		{"2.31.0", false},
		{"0.1.0", false},

		// Go pseudo-versions should not be treated as prerelease
		{"v0.0.0-20230101120000-abcdef123456", false},

		// Edge cases
		{"", false},
		{"  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := IsPrereleaseVersion(tt.version)
			if got != tt.want {
				t.Errorf("IsPrereleaseVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestOptionsWithDefaults(t *testing.T) {
	// Verify IncludePrerelease defaults to false (Go zero value)
	opts := Options{}.WithDefaults()
	if opts.IncludePrerelease {
		t.Error("IncludePrerelease should default to false")
	}

	// Verify explicit true is preserved
	opts = Options{IncludePrerelease: true}.WithDefaults()
	if !opts.IncludePrerelease {
		t.Error("IncludePrerelease should be preserved when set to true")
	}
}
