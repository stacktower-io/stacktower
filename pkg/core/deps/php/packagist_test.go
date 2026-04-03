package php

import (
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
	"github.com/matzehuels/stacktower/pkg/core/deps"
)

func TestNewResolver(t *testing.T) {
	r, err := Language.NewResolver(cache.NewNullCache(), deps.Options{CacheTTL: time.Minute})
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	if r == nil {
		t.Fatal("resolver is nil")
	}
}
