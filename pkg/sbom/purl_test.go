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
