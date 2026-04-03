package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNullCache(t *testing.T) {
	ctx := context.Background()
	c := NewNullCache()
	defer c.Close()

	// Get always returns miss
	data, hit, err := c.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if hit {
		t.Error("NullCache.Get should always return miss")
	}
	if data != nil {
		t.Error("NullCache.Get should return nil data")
	}

	// Set does nothing (no error)
	if err := c.Set(ctx, "key", []byte("value"), time.Hour); err != nil {
		t.Errorf("Set error: %v", err)
	}

	// Still a miss after Set
	_, hit, _ = c.Get(ctx, "key")
	if hit {
		t.Error("NullCache should not store data")
	}

	// Delete does nothing (no error)
	if err := c.Delete(ctx, "key"); err != nil {
		t.Errorf("Delete error: %v", err)
	}
}

func TestHash(t *testing.T) {
	// Test determinism
	h1 := Hash([]byte("hello"))
	h2 := Hash([]byte("hello"))
	if h1 != h2 {
		t.Error("Hash should be deterministic")
	}

	// Test different inputs produce different hashes
	h3 := Hash([]byte("world"))
	if h1 == h3 {
		t.Error("Different inputs should produce different hashes")
	}

	// Test hash length (SHA-256 produces 64 hex chars)
	if len(h1) != 64 {
		t.Errorf("Hash length should be 64, got %d", len(h1))
	}
}

func TestDefaultKeyer(t *testing.T) {
	k := NewDefaultKeyer()

	// HTTPKey
	httpKey := k.HTTPKey("pypi:", "requests")
	if httpKey != "http:pypi::requests" {
		t.Errorf("HTTPKey unexpected: %s", httpKey)
	}

	// GraphKey should include options in hash
	gk1 := k.GraphKey("python", "fastapi", GraphKeyOpts{MaxDepth: 10, MaxNodes: 100})
	gk2 := k.GraphKey("python", "fastapi", GraphKeyOpts{MaxDepth: 20, MaxNodes: 100})
	if gk1 == gk2 {
		t.Error("Different GraphKeyOpts should produce different keys")
	}

	// LayoutKey
	lk1 := k.LayoutKey("hash123", LayoutKeyOpts{VizType: "tower", Width: 800})
	lk2 := k.LayoutKey("hash123", LayoutKeyOpts{VizType: "nodelink", Width: 800})
	if lk1 == lk2 {
		t.Error("Different LayoutKeyOpts should produce different keys")
	}

	// ArtifactKey
	ak1 := k.ArtifactKey("hash123", ArtifactKeyOpts{Format: "svg", Style: "simple"})
	ak2 := k.ArtifactKey("hash123", ArtifactKeyOpts{Format: "png", Style: "simple"})
	if ak1 == ak2 {
		t.Error("Different ArtifactKeyOpts should produce different keys")
	}
}

func TestScopedKeyer(t *testing.T) {
	inner := NewDefaultKeyer()
	scoped := NewScopedKeyer(inner, "user:123:")

	// All keys should be prefixed
	httpKey := scoped.HTTPKey("npm:", "express")
	if httpKey != "user:123:http:npm::express" {
		t.Errorf("ScopedKeyer HTTPKey unexpected: %s", httpKey)
	}

	graphKey := scoped.GraphKey("rust", "serde", GraphKeyOpts{})
	if len(graphKey) < 15 || graphKey[:9] != "user:123:" {
		t.Errorf("ScopedKeyer GraphKey should be prefixed: %s", graphKey)
	}
}

func TestScopedKeyerNilInner(t *testing.T) {
	// Should use DefaultKeyer when inner is nil
	scoped := NewScopedKeyer(nil, "prefix:")
	key := scoped.HTTPKey("test:", "key")
	if key != "prefix:http:test::key" {
		t.Errorf("Unexpected key with nil inner: %s", key)
	}
}

func TestRetryableError(t *testing.T) {
	// Retryable(nil) returns nil
	if Retryable(nil) != nil {
		t.Error("Retryable(nil) should return nil")
	}

	// Non-nil error is wrapped
	err := Retryable(ErrNetwork)
	if err == nil {
		t.Fatal("Retryable should return wrapped error")
	}
	if !IsRetryable(err) {
		t.Error("IsRetryable should return true for wrapped error")
	}

	// Error message is preserved
	if err.Error() != ErrNetwork.Error() {
		t.Errorf("Error message should be preserved: %s", err.Error())
	}

	// Non-wrapped errors are not retryable
	if IsRetryable(ErrNotFound) {
		t.Error("IsRetryable should return false for unwrapped error")
	}
}

func TestRetryWithBackoff(t *testing.T) {
	ctx := context.Background()

	// Success on first try
	calls := 0
	err := RetryWithBackoff(ctx, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("Should succeed: %v", err)
	}
	if calls != 1 {
		t.Errorf("Should call once: %d", calls)
	}

	// Non-retryable error stops immediately
	calls = 0
	err = RetryWithBackoff(ctx, func() error {
		calls++
		return ErrNotFound
	})
	if err != ErrNotFound {
		t.Errorf("Should return non-retryable error: %v", err)
	}
	if calls != 1 {
		t.Errorf("Should not retry non-retryable error: %d", calls)
	}

	// Retryable error triggers retries
	calls = 0
	err = RetryWithBackoff(ctx, func() error {
		calls++
		if calls < 2 {
			return Retryable(ErrNetwork)
		}
		return nil
	})
	if err != nil {
		t.Errorf("Should succeed after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("Should retry once: %d", calls)
	}
}

func TestRetryWithBackoffContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := RetryWithBackoff(ctx, func() error {
		return Retryable(ErrNetwork)
	})
	if err != context.Canceled {
		t.Errorf("Should return context error: %v", err)
	}
}

type retryAfterErr struct {
	err        error
	retryAfter int
}

func (e *retryAfterErr) Error() string          { return e.err.Error() }
func (e *retryAfterErr) Unwrap() error          { return e.err }
func (e *retryAfterErr) RetryAfterSeconds() int { return e.retryAfter }

func TestRetryWithBackoffRegistryHonorsRetryAfter(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	calls := 0

	err := RetryWithBackoffRegistry(ctx, "test", func() error {
		calls++
		if calls == 1 {
			return Retryable(&retryAfterErr{err: ErrNetwork, retryAfter: 1})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("expected retry delay to honor retry-after, elapsed=%v", elapsed)
	}
}

func TestRetryAfterFromError(t *testing.T) {
	err := Retryable(&retryAfterErr{err: errors.New("rate limited"), retryAfter: 42})
	got, ok := retryAfterFromError(err)
	if !ok {
		t.Fatalf("expected retry-after to be detected")
	}
	if got != 42 {
		t.Fatalf("retry-after=%d, want 42", got)
	}
}
