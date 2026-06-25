package gitlab

import (
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
)

func TestNewClient(t *testing.T) {
	c := NewClient(cache.NewNullCache(), "test-token", time.Hour)
	if c.Client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestExtractURL(t *testing.T) {
	tests := []struct {
		name      string
		urls      map[string]string
		home      string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "simple owner/repo",
			urls:      map[string]string{"Source": "https://gitlab.com/owner/repo"},
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "nested group",
			urls:      map[string]string{"Source": "https://gitlab.com/group/subgroup/project"},
			wantOwner: "group/subgroup",
			wantRepo:  "project",
			wantOK:    true,
		},
		{
			name:      "deeply nested group",
			urls:      map[string]string{"Repository": "https://gitlab.com/a/b/c/d"},
			wantOwner: "a/b/c",
			wantRepo:  "d",
			wantOK:    true,
		},
		{
			name:      "git suffix stripped",
			urls:      map[string]string{"Source": "https://gitlab.com/owner/repo.git"},
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "trailing slash",
			urls:      map[string]string{"Source": "https://gitlab.com/owner/repo/"},
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "query string excluded",
			urls:      map[string]string{"Source": "https://gitlab.com/owner/repo?tab=activity"},
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "from homepage fallback",
			home:      "http://gitlab.com/baz/qux",
			wantOwner: "baz",
			wantRepo:  "qux",
			wantOK:    true,
		},
		{
			name:   "dash-path URL ignored",
			urls:   map[string]string{"Source": "https://gitlab.com/group/project/-/blob/main/README.md"},
			wantOK: false,
		},
		{
			name:   "single path segment",
			urls:   map[string]string{"Source": "https://gitlab.com/onlyowner"},
			wantOK: false,
		},
		{
			name:   "non-gitlab url",
			urls:   map[string]string{"Homepage": "https://example.com/owner/repo"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ExtractURL(tt.urls, tt.home)
			if ok != tt.wantOK {
				t.Fatalf("got ok=%v, want %v (owner=%q repo=%q)", ok, tt.wantOK, owner, repo)
			}
			if !ok {
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("got owner %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("got repo %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}
