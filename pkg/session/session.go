// Package session provides session management for authenticated users.
//
// This package defines interfaces for session storage and OAuth state management.
// The CLI uses a file-based store; other backends (memory, redis) can be
// implemented against the [Store] and [StateStore] interfaces.
//
// # Architecture
//
// Sessions store user authentication data (access tokens, user info) with
// automatic expiration. The Store interface supports:
//   - Get/Set/Delete operations
//   - Automatic expiration checking
//   - Cleanup of expired sessions
//
// OAuth state tokens provide CSRF protection during the OAuth flow. The
// StateStore interface supports:
//   - Token generation with TTL
//   - Single-use validation (tokens are deleted after validation)
//
// # Usage
//
// Create a session store for CLI use:
//
//	store, err := session.NewCLIStore()  // Uses ~/.config/stacktower/sessions/
//
// Manage sessions:
//
//	// Create session
//	sess, err := session.New(accessToken, user, session.DefaultTTL)
//	if err != nil {
//	    return err
//	}
//	store.SaveSession(ctx, sess)
//
//	// Retrieve session
//	sess, err := store.GetSession(ctx)
//	if err != nil {
//	    return err
//	}
//	if sess == nil || sess.IsExpired() {
//	    // Session not found or expired
//	}
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/integrations/github"
)

// Sentinel errors for session operations.
var (
	// ErrNotFound is returned when a session does not exist.
	// Shared with cache/integrations for consistent errors.Is checks.
	ErrNotFound = cache.ErrNotFound

	// ErrExpired is returned when a session has exceeded its TTL.
	ErrExpired = errors.New("expired")

	// ErrInvalidState is returned when an OAuth state token is invalid or already used.
	ErrInvalidState = errors.New("invalid or expired state token")
)

// Session stores user session data.
type Session struct {
	ID          string       `json:"id"`
	AccessToken string       `json:"access_token"`
	User        *github.User `json:"user"`
	ExpiresAt   time.Time    `json:"expires_at"`
	CreatedAt   time.Time    `json:"created_at"`
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// UserID returns a storage-compatible user identifier.
// Format: "github:{id}" to namespace by auth provider.
// This format is used in cache keys and document ownership.
func (s *Session) UserID() string {
	if s == nil || s.User == nil {
		return ""
	}
	return fmt.Sprintf("github:%d", s.User.ID)
}

// Store is the interface for session storage backends.
type Store interface {
	// Get retrieves a session by ID.
	// Returns nil, nil if the session doesn't exist.
	// Returns nil, ErrExpired if the session exists but has expired.
	Get(ctx context.Context, sessionID string) (*Session, error)

	// Set stores a session.
	Set(ctx context.Context, session *Session) error

	// Delete removes a session.
	Delete(ctx context.Context, sessionID string) error

	// Cleanup removes expired sessions (optional, may be no-op for Redis).
	Cleanup(ctx context.Context) error
}

// StateStore manages OAuth state tokens for CSRF protection.
// State tokens are short-lived (typically 10 minutes) and single-use.
// For multi-instance deployments, use Redis to share state across instances.
type StateStore interface {
	// Generate creates a new state token and stores it with the given TTL.
	// Returns the generated state token.
	Generate(ctx context.Context, ttl time.Duration) (string, error)

	// Validate checks if a state token is valid and removes it (single-use).
	// Returns true if the token was valid and not expired.
	Validate(ctx context.Context, state string) (bool, error)

	// Cleanup removes expired state tokens (optional, may be no-op for Redis).
	Cleanup(ctx context.Context) error
}

// Default durations.
const (
	// DefaultTTL is the default session duration.
	DefaultTTL = 24 * time.Hour

	// DefaultStateTTL is the default OAuth state token duration.
	DefaultStateTTL = 10 * time.Minute
)

// GenerateID creates a cryptographically secure random session ID.
func GenerateID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// GenerateState creates a cryptographically secure random state token.
func GenerateState() (string, error) {
	return GenerateID() // Same implementation, different semantic meaning
}

// New creates a new session with the given token and user.
func New(accessToken string, user *github.User, ttl time.Duration) (*Session, error) {
	id, err := GenerateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	return &Session{
		ID:          id,
		AccessToken: accessToken,
		User:        user,
		ExpiresAt:   now.Add(ttl),
		CreatedAt:   now,
	}, nil
}
