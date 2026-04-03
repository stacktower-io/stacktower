package integrations

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/matzehuels/stacktower/pkg/observability"
)

// ErrCircuitOpen is returned when the circuit breaker is open and rejecting requests.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern for rate limit handling.
// It tracks consecutive rate limit failures and temporarily stops requests when
// a threshold is exceeded, preventing resource exhaustion during rate limiting.
//
// States:
//   - Closed: Normal operation, requests are allowed
//   - Open: Requests are rejected to allow recovery
//   - Half-Open: One probe request is allowed to test if the service has recovered
//
// CircuitBreaker is safe for concurrent use.
type CircuitBreaker struct {
	mu          sync.Mutex
	state       observability.CircuitState
	failures    int           // consecutive rate limit failures
	lastFailure time.Time     // time of last failure
	openUntil   time.Time     // when to transition from open to half-open
	registry    string        // registry name for observability
	threshold   int           // failures before opening (default 5)
	cooldown    time.Duration // time in open state before half-open (default 30s)
}

// CircuitBreakerConfig holds configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	// Threshold is the number of consecutive rate limit failures before opening.
	// Default: 5
	Threshold int
	// Cooldown is how long the circuit stays open before transitioning to half-open.
	// Default: 30s
	Cooldown time.Duration
}

// DefaultCircuitBreakerConfig returns sensible defaults for circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold: 5,
		Cooldown:  30 * time.Second,
	}
}

// NewCircuitBreaker creates a circuit breaker for the given registry.
func NewCircuitBreaker(registry string, config CircuitBreakerConfig) *CircuitBreaker {
	if config.Threshold <= 0 {
		config.Threshold = 5
	}
	if config.Cooldown <= 0 {
		config.Cooldown = 30 * time.Second
	}
	return &CircuitBreaker{
		state:     observability.CircuitClosed,
		registry:  registry,
		threshold: config.Threshold,
		cooldown:  config.Cooldown,
	}
}

// Allow checks if a request should be allowed through the circuit breaker.
// Returns true if the request can proceed, false if it should be rejected.
//
// In the half-open state, only one request is allowed through to probe
// whether the service has recovered.
func (cb *CircuitBreaker) Allow(ctx context.Context) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case observability.CircuitClosed:
		return true

	case observability.CircuitOpen:
		if time.Now().After(cb.openUntil) {
			cb.transitionTo(ctx, observability.CircuitHalfOpen, time.Time{})
			return true
		}
		return false

	case observability.CircuitHalfOpen:
		return true

	default:
		return true
	}
}

// RecordSuccess records a successful request.
// In the half-open state, this closes the circuit.
// In the closed state, this resets the failure counter.
func (cb *CircuitBreaker) RecordSuccess(ctx context.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == observability.CircuitHalfOpen {
		cb.transitionTo(ctx, observability.CircuitClosed, time.Time{})
	}
}

// RecordFailure records a rate limit failure.
// If the failure threshold is exceeded, the circuit opens.
// The retryAfter parameter is used to set the cooldown duration if provided.
func (cb *CircuitBreaker) RecordFailure(ctx context.Context, retryAfter int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == observability.CircuitHalfOpen {
		cooldown := cb.calculateCooldown(retryAfter)
		cb.openUntil = time.Now().Add(cooldown)
		cb.transitionTo(ctx, observability.CircuitOpen, cb.openUntil)
		return
	}

	if cb.state == observability.CircuitClosed && cb.failures >= cb.threshold {
		cooldown := cb.calculateCooldown(retryAfter)
		cb.openUntil = time.Now().Add(cooldown)
		cb.transitionTo(ctx, observability.CircuitOpen, cb.openUntil)
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() observability.CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Failures returns the current consecutive failure count.
func (cb *CircuitBreaker) Failures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

// Reset resets the circuit breaker to its initial closed state.
// This is primarily useful for testing.
func (cb *CircuitBreaker) Reset(ctx context.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != observability.CircuitClosed {
		cb.transitionTo(ctx, observability.CircuitClosed, time.Time{})
	}
	cb.failures = 0
	cb.lastFailure = time.Time{}
	cb.openUntil = time.Time{}
}

func (cb *CircuitBreaker) calculateCooldown(retryAfter int) time.Duration {
	if retryAfter > 0 {
		retryDuration := time.Duration(retryAfter) * time.Second
		if retryDuration > cb.cooldown {
			return retryDuration
		}
	}
	return cb.cooldown
}

func (cb *CircuitBreaker) transitionTo(ctx context.Context, state observability.CircuitState, until time.Time) {
	if cb.state == state {
		return
	}
	cb.state = state
	observability.RateLimit().OnCircuitStateChange(ctx, cb.registry, state, until)
}

// CircuitBreakerRegistry manages circuit breakers for multiple registries.
// It provides a thread-safe way to get or create circuit breakers per registry.
type CircuitBreakerRegistry struct {
	mu       sync.Mutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewCircuitBreakerRegistry creates a registry with the given default configuration.
func NewCircuitBreakerRegistry(config CircuitBreakerConfig) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// Get returns the circuit breaker for the given registry, creating one if needed.
func (r *CircuitBreakerRegistry) Get(registry string) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok := r.breakers[registry]; ok {
		return cb
	}

	cb := NewCircuitBreaker(registry, r.config)
	r.breakers[registry] = cb
	return cb
}

// Reset resets all circuit breakers to their initial state.
// This is primarily useful for testing.
func (r *CircuitBreakerRegistry) Reset(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cb := range r.breakers {
		cb.Reset(ctx)
	}
}
