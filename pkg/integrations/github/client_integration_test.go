//go:build integration

package github

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestFetch_Integration(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping integration test")
	}

	client := NewClient(cache.NewNullCache(), token, time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		owner   string
		repo    string
		wantErr bool
	}{
		{"golang/go", "golang", "go", false},
		{"nonexistent", "nonexistent-owner-12345", "nonexistent-repo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := client.Fetch(ctx, tt.owner, tt.repo, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("Fetch(%q, %q) error = %v, wantErr %v", tt.owner, tt.repo, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if metrics.RepoURL == "" {
					t.Error("RepoURL should not be empty")
				}
				if metrics.Stars < 0 {
					t.Error("Stars should not be negative")
				}
			}
		})
	}
}

func TestExtractURL(t *testing.T) {
	tests := []struct {
		name      string
		urls      map[string]string
		homepage  string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "github URL in map",
			urls:      map[string]string{"Source": "https://github.com/owner/repo"},
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "github URL in homepage",
			urls:      nil,
			homepage:  "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:   "no github URL",
			urls:   map[string]string{"homepage": "https://example.com"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ExtractURL(tt.urls, tt.homepage)
			if ok != tt.wantOK {
				t.Errorf("ExtractURL() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
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
