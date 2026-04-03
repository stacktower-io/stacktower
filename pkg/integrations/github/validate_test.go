package github

import "testing"

func TestParseRepoRef(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		// plain owner/repo
		{"fastapi/fastapi", "fastapi", "fastapi", false},
		{"matzehuels/stacktower", "matzehuels", "stacktower", false},
		// full HTTPS URL
		{"https://github.com/fastapi/fastapi", "fastapi", "fastapi", false},
		{"http://github.com/fastapi/fastapi", "fastapi", "fastapi", false},
		// without scheme
		{"github.com/fastapi/fastapi", "fastapi", "fastapi", false},
		// trailing slash and .git suffix
		{"https://github.com/fastapi/fastapi.git", "fastapi", "fastapi", false},
		{"github.com/fastapi/fastapi/", "fastapi", "fastapi", false},
		// error cases
		{"just-owner", "", "", true},
		{"", "", "", true},
		{"-invalid/repo", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo, err := ParseRepoRef(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRepoRef(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
			}
		})
	}
}
