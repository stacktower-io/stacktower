package python

import (
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
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
