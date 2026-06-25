package integrations

import (
	"regexp"
	"strings"
	"testing"
)

// TestNormalizePkgName_PEP503 covers the full PEP 503 rule: runs of
// hyphens, underscores, and dots collapse to a single hyphen.
// (Basic lowercase/trim cases are covered by TestNormalizePkgName.)
func TestNormalizePkgName_PEP503(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"zope.interface", "zope-interface"},
		{"backports.zoneinfo", "backports-zoneinfo"},
		{"my--package", "my-package"},
		{"my__package", "my-package"},
		{"a-_.b", "a-b"},
		{"Mixed_Separators.Here--Now", "mixed-separators-here-now"},
	}

	for _, tt := range tests {
		if got := NormalizePkgName(tt.input); got != tt.want {
			t.Errorf("NormalizePkgName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractRepoURLFunc(t *testing.T) {
	// Matcher that handles multi-segment namespaces (GitLab-style).
	match := func(u string) (string, string, bool) {
		const prefix = "https://example.com/"
		if !strings.HasPrefix(u, prefix) {
			return "", "", false
		}
		segs := strings.Split(strings.Trim(strings.TrimPrefix(u, prefix), "/"), "/")
		if len(segs) < 2 {
			return "", "", false
		}
		return strings.Join(segs[:len(segs)-1], "/"), segs[len(segs)-1], true
	}

	owner, repo, ok := ExtractRepoURLFunc(match, map[string]string{
		"Source": "https://example.com/group/sub/proj",
	}, "")
	if !ok || owner != "group/sub" || repo != "proj" {
		t.Errorf("got owner=%q repo=%q ok=%v, want group/sub proj true", owner, repo, ok)
	}

	// Sponsor URLs are skipped even if the matcher would accept them.
	_, _, ok = ExtractRepoURLFunc(match, map[string]string{
		"Funding": "https://example.com/sponsors/someone",
	}, "")
	if ok {
		t.Error("sponsors URL should be skipped")
	}

	// Homepage fallback.
	owner, repo, ok = ExtractRepoURLFunc(match, nil, "https://example.com/me/thing")
	if !ok || owner != "me" || repo != "thing" {
		t.Errorf("homepage fallback: got owner=%q repo=%q ok=%v", owner, repo, ok)
	}
}

func TestExtractRepoURL(t *testing.T) {
	githubPattern := regexp.MustCompile(`https?://github\.com/([^/]+)/([^/]+)`)
	gitlabPattern := regexp.MustCompile(`https?://gitlab\.com/([^/]+)/([^/]+)`)

	tests := []struct {
		name        string
		pattern     *regexp.Regexp
		projectURLs map[string]string
		homepage    string
		wantOwner   string
		wantRepo    string
		wantOK      bool
	}{
		{
			name:        "github from project urls",
			pattern:     githubPattern,
			projectURLs: map[string]string{"Source": "https://github.com/foo/bar"},
			wantOwner:   "foo",
			wantRepo:    "bar",
			wantOK:      true,
		},
		{
			name:      "github from homepage",
			pattern:   githubPattern,
			homepage:  "http://github.com/baz/qux",
			wantOwner: "baz",
			wantRepo:  "qux",
			wantOK:    true,
		},
		{
			name:        "gitlab from project urls",
			pattern:     gitlabPattern,
			projectURLs: map[string]string{"Repository": "https://gitlab.com/acme/widget"},
			wantOwner:   "acme",
			wantRepo:    "widget",
			wantOK:      true,
		},
		{
			name:        "no match",
			pattern:     githubPattern,
			projectURLs: map[string]string{"Homepage": "https://example.com"},
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ExtractRepoURL(tt.pattern, tt.projectURLs, tt.homepage)
			if ok != tt.wantOK {
				t.Errorf("got ok=%v, want %v", ok, tt.wantOK)
			}
			if ok {
				if owner != tt.wantOwner {
					t.Errorf("got owner=%s, want %s", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("got repo=%s, want %s", repo, tt.wantRepo)
				}
			}
		})
	}
}
