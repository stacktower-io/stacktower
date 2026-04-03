package cache

import (
	"context"
	"errors"
	"time"

	"github.com/matzehuels/stacktower/pkg/observability"
)

// Sentinel errors for caching operations.
var (
	// ErrNotFound is returned when a requested item does not exist.
	ErrNotFound = errors.New("not found")

	// ErrNetwork is returned for HTTP failures (timeouts, connection errors, 5xx responses).
	ErrNetwork = errors.New("network error")

	// ErrUnauthorized is returned when authentication fails (HTTP 401/403).
	// This typically means the API token is invalid, expired, or revoked.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrCacheMiss is returned when an item is not found in cache.
	ErrCacheMiss = errors.New("cache miss")
)

// RetryableError wraps an error to indicate it should trigger a retry.
type RetryableError struct{ Err error }

type retryAfterProvider interface {
	RetryAfterSeconds() int
}

// Retryable wraps an error as a RetryableError.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err}
}

// Error returns the error message of the wrapped error.
func (e *RetryableError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *RetryableError) Unwrap() error { return e.Err }

// IsRetryable checks if an error is wrapped with RetryableError.
func IsRetryable(err error) bool {
	var re *RetryableError
	return errors.As(err, &re)
}

// RetryWithBackoff retries fn up to 3 times with exponential backoff.
// Only errors wrapped with Retryable will trigger retries.
func RetryWithBackoff(ctx context.Context, fn func() error) error {
	return RetryWithBackoffRegistry(ctx, "", fn)
}

// RetryWithBackoffRegistry retries fn up to 3 times with exponential backoff,
// emitting observability hooks with the registry name for each retry.
func RetryWithBackoffRegistry(ctx context.Context, registry string, fn func() error) error {
	const attempts = 3
	baseDelay := time.Second
	var lastErr error

	for i := 0; i < attempts; i++ {
		if err := fn(); err == nil {
			return nil
		} else if lastErr = err; !IsRetryable(err) {
			return err
		}

		if i < attempts-1 {
			delay := baseDelay
			if retryAfter, ok := retryAfterFromError(lastErr); ok && retryAfter > 0 {
				delay = time.Duration(retryAfter) * time.Second
			}
			observability.RateLimit().OnRetry(ctx, registry, i+1, delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				baseDelay *= 2
			}
		}
	}
	return lastErr
}

func retryAfterFromError(err error) (int, bool) {
	var p retryAfterProvider
	if errors.As(err, &p) {
		return p.RetryAfterSeconds(), true
	}
	return 0, false
}
