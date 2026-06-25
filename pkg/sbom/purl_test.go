package sbom

import "testing"

func TestBuildPURL(t *testing.T) {
	tests := []struct {
		language string
		name     string
		version  string
		want     string
	}{
		{"python", "flask", "3.1.0", "pkg:pypi/flask@3.1.0"},
		{"python", "werkzeug", "3.0.0", "pkg:pypi/werkzeug@3.0.0"},
		{"javascript", "express", "4.18.0", "pkg:npm/express@4.18.0"},
		{"javascript", "@angular/core", "17.0.0", "pkg:npm/%40angular/core@17.0.0"},
		{"rust", "serde", "1.0.200", "pkg:cargo/serde@1.0.200"},
		{"go", "golang.org/x/sync", "0.7.0", "pkg:golang/golang.org/x/sync@0.7.0"},
		{"ruby", "rails", "7.1.0", "pkg:gem/rails@7.1.0"},
		{"php", "laravel/framework", "10.0.0", "pkg:composer/laravel/framework@10.0.0"},
		{"java", "org.apache.commons:commons-lang3", "3.14.0", "pkg:maven/org.apache.commons/commons-lang3@3.14.0"},
		{"python", "requests", "", "pkg:pypi/requests"},
		{"unknown", "foo", "1.0", ""},
		// Percent-encoding of special characters in names and versions
		{"javascript", "weird name", "1.0.0", "pkg:npm/weird%20name@1.0.0"},
		{"javascript", "left-pad", "1.0.0+build.1", "pkg:npm/left-pad@1.0.0+build.1"},
		{"go", "github.com/user/repo", "v1.0.0-0.20230101120000-abcdef123456", "pkg:golang/github.com/user/repo@v1.0.0-0.20230101120000-abcdef123456"},
		{"javascript", "has@sign", "1.0.0", "pkg:npm/has%40sign@1.0.0"},
		{"python", "pkg", "1.0:beta", "pkg:pypi/pkg@1.0%3Abeta"},
	}

	for _, tt := range tests {
		t.Run(tt.language+"/"+tt.name, func(t *testing.T) {
			got := BuildPURL(tt.language, tt.name, tt.version)
			if got != tt.want {
				t.Errorf("BuildPURL(%q, %q, %q) = %q, want %q",
					tt.language, tt.name, tt.version, got, tt.want)
			}
		})
	}
}
