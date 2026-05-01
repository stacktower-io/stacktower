package cli

import (
	"context"
	"errors"
	"time"

	"github.com/stacktower-io/stacktower/pkg/integrations/github"
	"github.com/stacktower-io/stacktower/pkg/session"
)

// sessionTTL is the duration for CLI sessions (30 days).
const sessionTTL = 30 * 24 * time.Hour

// sessionExpiryWarning is how far before expiry `whoami` starts surfacing a
// pre-expiry refresh hint. Tuned to give users a few days' runway without
// being noisy on most day-to-day invocations.
const sessionExpiryWarning = 3 * 24 * time.Hour

// loadGitHubSession loads the GitHub session from disk.
func loadGitHubSession(ctx context.Context) (*session.Session, error) {
	store, err := session.NewCLIStore()
	if err != nil {
		return nil, WrapSystemError(err, "failed to open session store", "Check file permissions for ~/.config/stacktower/sessions/")
	}

	sess, err := store.GetSession(ctx)
	if err != nil {
		return nil, WrapSystemError(err, "failed to read session", "Try 'stacktower github logout' and re-login.")
	}
	if sess == nil {
		return nil, WrapUserError(ErrNotLoggedIn, "not logged in", "Run 'stacktower github login' first.")
	}

	return sess, nil
}

func saveGitHubSession(ctx context.Context, token *github.OAuthToken, user *github.User) (*session.Session, error) {
	store, err := session.NewCLIStore()
	if err != nil {
		return nil, WrapSystemError(err, "failed to open session store", "Check file permissions for ~/.config/stacktower/sessions/")
	}

	sess, err := session.New(token.AccessToken, user, sessionTTL)
	if err != nil {
		return nil, WrapSystemError(err, "failed to create session", "")
	}

	if err := store.SaveSession(ctx, sess); err != nil {
		return nil, WrapSystemError(err, "failed to save session", "Check file permissions for ~/.config/stacktower/sessions/")
	}

	return sess, nil
}

func deleteGitHubSession(ctx context.Context) error {
	store, err := session.NewCLIStore()
	if err != nil {
		return WrapSystemError(err, "failed to open session store", "Check file permissions for ~/.config/stacktower/sessions/")
	}
	if err := store.DeleteSession(ctx); err != nil {
		return WrapSystemError(err, "failed to delete session", "Check file permissions for ~/.config/stacktower/sessions/")
	}
	return nil
}

func isNotLoggedInError(err error) bool {
	return errors.Is(err, ErrNotLoggedIn)
}
