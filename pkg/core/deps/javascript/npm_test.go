package javascript

import (
	"testing"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

func TestNewResolver(t *testing.T) {
	r, err := Language.NewResolver(cache.NewNullCache(), deps.Options{CacheTTL: time.Hour})
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	if r == nil {
		t.Error("resolver not initialized")
	}
}

func TestSortVersionsDescending(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     []string
	}{
		{
			name: "double digit major sorts above single digit",
			// Lexicographic sort would put 9.x above 10.x
			versions: []string{"9.0.0", "10.0.0", "9.5.1", "10.2.0", "1.0.0"},
			want:     []string{"10.2.0", "10.0.0", "9.5.1", "9.0.0", "1.0.0"},
		},
		{
			name:     "double digit minor and patch",
			versions: []string{"1.9.0", "1.10.0", "1.2.10", "1.2.9"},
			want:     []string{"1.10.0", "1.9.0", "1.2.10", "1.2.9"},
		},
		{
			name:     "prerelease sorts below release",
			versions: []string{"2.0.0-beta.1", "2.0.0", "2.0.0-alpha.1"},
			want:     []string{"2.0.0", "2.0.0-beta.1", "2.0.0-alpha.1"},
		},
		{
			name:     "unparseable versions sort last",
			versions: []string{"not-a-version", "1.0.0", "2.0.0"},
			want:     []string{"2.0.0", "1.0.0", "not-a-version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make([]string, len(tt.versions))
			copy(got, tt.versions)
			sortVersionsDescending(got)
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("sorted[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
