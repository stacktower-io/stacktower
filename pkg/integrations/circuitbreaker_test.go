package integrations

import (
	"context"
	"testing"
	"time"

	"github.com/matzehuels/stacktower/pkg/observability"
)

func TestCircuitBreakerInitialState(t *testing.T) {
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig())
	if cb.State() != observability.CircuitClosed {
		t.Errorf("expected initial state to be closed, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected initial failures to be 0, got %d", cb.Failures())
	}
}

func TestCircuitBreakerAllowsRequestsWhenClosed(t *testing.T) {
	ctx := context.Background()
	cb := NewCircuitBreaker("test", DefaultCircuitBreakerConfig())

	for i := 0; i < 10; i++ {
		if !cb.Allow(ctx) {
			t.Errorf("expected request %d to be allowed when circuit is closed", i)
		}
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 3,
		Cooldown:  time.Minute,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitClosed {
		t.Errorf("expected circuit to remain closed after 2 failures, got %s", cb.State())
	}

	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitOpen {
		t.Errorf("expected circuit to open after 3 failures, got %s", cb.State())
	}
}

func TestCircuitBreakerRejectsWhenOpen(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  time.Minute,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	if cb.Allow(ctx) {
		t.Error("expected request to be rejected when circuit is open")
	}
}

func TestCircuitBreakerTransitionsToHalfOpenAfterCooldown(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	time.Sleep(20 * time.Millisecond)

	if !cb.Allow(ctx) {
		t.Error("expected request to be allowed after cooldown")
	}
	if cb.State() != observability.CircuitHalfOpen {
		t.Errorf("expected circuit to be half-open, got %s", cb.State())
	}
}

func TestCircuitBreakerClosesOnSuccessInHalfOpen(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	time.Sleep(20 * time.Millisecond)
	cb.Allow(ctx)

	if cb.State() != observability.CircuitHalfOpen {
		t.Fatalf("expected circuit to be half-open, got %s", cb.State())
	}

	cb.RecordSuccess(ctx)
	if cb.State() != observability.CircuitClosed {
		t.Errorf("expected circuit to close after success in half-open, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected failures to reset to 0, got %d", cb.Failures())
	}
}

func TestCircuitBreakerReopensOnFailureInHalfOpen(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	time.Sleep(20 * time.Millisecond)
	cb.Allow(ctx)

	if cb.State() != observability.CircuitHalfOpen {
		t.Fatalf("expected circuit to be half-open")
	}

	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitOpen {
		t.Errorf("expected circuit to reopen after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreakerHonorsRetryAfter(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  10 * time.Millisecond,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 60)

	if cb.Allow(ctx) {
		t.Error("expected request to be rejected immediately after opening")
	}
}

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 3,
		Cooldown:  time.Minute,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	cb.RecordFailure(ctx, 0)
	if cb.Failures() != 2 {
		t.Errorf("expected 2 failures, got %d", cb.Failures())
	}

	cb.RecordSuccess(ctx)
	if cb.Failures() != 0 {
		t.Errorf("expected failures to reset to 0 after success, got %d", cb.Failures())
	}

	cb.RecordFailure(ctx, 0)
	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitClosed {
		t.Errorf("expected circuit to remain closed after 2 new failures, got %s", cb.State())
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	ctx := context.Background()
	config := CircuitBreakerConfig{
		Threshold: 1,
		Cooldown:  time.Minute,
	}
	cb := NewCircuitBreaker("test", config)

	cb.RecordFailure(ctx, 0)
	if cb.State() != observability.CircuitOpen {
		t.Fatalf("expected circuit to be open")
	}

	cb.Reset(ctx)
	if cb.State() != observability.CircuitClosed {
		t.Errorf("expected circuit to be closed after reset, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Errorf("expected failures to be 0 after reset, got %d", cb.Failures())
	}
}

func TestCircuitBreakerRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewCircuitBreakerRegistry(DefaultCircuitBreakerConfig())

	cb1 := registry.Get("npm")
	cb2 := registry.Get("pypi")
	cb1Again := registry.Get("npm")

	if cb1 != cb1Again {
		t.Error("expected same circuit breaker for same registry")
	}
	if cb1 == cb2 {
		t.Error("expected different circuit breakers for different registries")
	}

	config := CircuitBreakerConfig{Threshold: 1, Cooldown: time.Minute}
	registry2 := NewCircuitBreakerRegistry(config)
	cb := registry2.Get("test")
	cb.RecordFailure(ctx, 0)

	if cb.State() != observability.CircuitOpen {
		t.Error("expected circuit to open after 1 failure with threshold 1")
	}

	registry2.Reset(ctx)
	if cb.State() != observability.CircuitClosed {
		t.Error("expected circuit to be closed after registry reset")
	}
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	if config.Threshold != 5 {
		t.Errorf("expected default threshold 5, got %d", config.Threshold)
	}
	if config.Cooldown != 30*time.Second {
		t.Errorf("expected default cooldown 30s, got %v", config.Cooldown)
	}
}

func TestNewCircuitBreakerWithInvalidConfig(t *testing.T) {
	cb := NewCircuitBreaker("test", CircuitBreakerConfig{
		Threshold: -1,
		Cooldown:  -1,
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		cb.RecordFailure(ctx, 0)
	}

	if cb.State() != observability.CircuitOpen {
		t.Error("expected circuit to open with corrected default threshold")
	}
}
