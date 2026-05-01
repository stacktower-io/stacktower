// Package sessiontest provides test helpers for the session package.
package sessiontest

import (
	"time"

	"github.com/stacktower-io/stacktower/pkg/integrations/github"
	"github.com/stacktower-io/stacktower/pkg/session"
)

// MockLocal creates a mock session for local development without authentication.
// The mock user has ID "local" and no GitHub access token.
func MockLocal() *session.Session {
	now := time.Now()
	return &session.Session{
		ID:          "local-session",
		AccessToken: "",
		User: &github.User{
			ID:        0,
			Login:     "local",
			Name:      "Local User",
			AvatarURL: "",
		},
		ExpiresAt: now.Add(365 * 24 * time.Hour),
		CreatedAt: now,
	}
}
