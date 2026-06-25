package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return store, dir
}

func testSession(id string, ttl time.Duration) *Session {
	now := time.Now()
	return &Session{
		ID:          id,
		AccessToken: "token-123",
		ExpiresAt:   now.Add(ttl),
		CreatedAt:   now,
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, testSession("github", time.Hour)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	sess, err := store.Get(ctx, "github")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sess == nil || sess.AccessToken != "token-123" {
		t.Errorf("Get = %+v, want access token token-123", sess)
	}

	if err := store.Delete(ctx, "github"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	sess, err = store.Get(ctx, "github")
	if err != nil || sess != nil {
		t.Errorf("Get after delete = (%+v, %v), want (nil, nil)", sess, err)
	}
}

func TestFileStoreExpiredSession(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, testSession("github", -time.Hour)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	sess, err := store.Get(ctx, "github")
	if !errors.Is(err, ErrExpired) {
		t.Errorf("Get expired session: err = %v, want ErrExpired", err)
	}
	if sess != nil {
		t.Errorf("Get expired session: sess = %+v, want nil", sess)
	}

	// Expired session file must be cleaned up.
	if _, statErr := os.Stat(filepath.Join(dir, "github.json")); !os.IsNotExist(statErr) {
		t.Error("expired session file should have been removed")
	}
}

func TestFileStoreRejectsPathTraversal(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()

	// Plant a file outside the session dir that a traversal would hit.
	outside := filepath.Join(filepath.Dir(dir), "victim.json")
	if err := os.WriteFile(outside, []byte(`{"id":"victim"}`), 0600); err != nil {
		t.Fatalf("plant victim file: %v", err)
	}

	for _, id := range []string{
		"../victim",
		"../../etc/passwd",
		"a/b",
		"a\\b",
		".hidden",
		"",
	} {
		if _, err := store.Get(ctx, id); err == nil {
			t.Errorf("Get(%q) should reject invalid session ID", id)
		}
		if err := store.Set(ctx, testSession(id, time.Hour)); err == nil {
			t.Errorf("Set(%q) should reject invalid session ID", id)
		}
		if err := store.Delete(ctx, id); err == nil {
			t.Errorf("Delete(%q) should reject invalid session ID", id)
		}
	}

	// Victim file untouched.
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("victim file should be untouched: %v", err)
	}
}

func TestFileStoreAcceptsGeneratedIDs(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	id, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	if err := store.Set(ctx, testSession(id, time.Hour)); err != nil {
		t.Fatalf("Set with generated ID %q: %v", id, err)
	}
	sess, err := store.Get(ctx, id)
	if err != nil || sess == nil {
		t.Fatalf("Get with generated ID: (%+v, %v)", sess, err)
	}
}

func TestFileStoreAtomicWriteNoTempLeftovers(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, testSession("github", time.Hour)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "github.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("session dir = %v, want exactly [github.json]", names)
	}

	info, err := os.Stat(filepath.Join(dir, "github.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("session file perm = %o, want 0600 (contains access token)", perm)
	}
}
