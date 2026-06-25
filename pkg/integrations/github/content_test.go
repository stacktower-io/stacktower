package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestContentClient(t *testing.T, handler http.Handler) *ContentClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewContentClient("test-token")
	c.baseURL = srv.URL
	return c
}

func TestListContents_EscapesPathAndRef(t *testing.T) {
	var gotPath, gotRef string
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotRef = r.URL.Query().Get("ref")
		_, _ = w.Write([]byte("[]"))
	}))

	_, err := c.ListContents(context.Background(), "owner", "repo", "dir with space/sub#dir", "feature/my branch")
	if err != nil {
		t.Fatalf("ListContents: %v", err)
	}
	if !strings.Contains(gotPath, "dir%20with%20space/sub%23dir") {
		t.Errorf("path not escaped: %s", gotPath)
	}
	if gotRef != "feature/my branch" {
		t.Errorf("ref not round-tripped correctly: %q", gotRef)
	}
}

func TestFetchFile_EscapesPathAndRef(t *testing.T) {
	var gotPath string
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewEncoder(w).Encode(apiContentResponse{Path: "x", Content: "aGVsbG8="})
	}))

	fc, err := c.FetchFile(context.Background(), "owner", "repo", "a b/c.txt", "v1.0")
	if err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	if fc.Content != "hello" {
		t.Errorf("content = %q, want hello", fc.Content)
	}
	if !strings.Contains(gotPath, "a%20b/c.txt") {
		t.Errorf("path not escaped: %s", gotPath)
	}
}

func TestGetTree_Truncated(t *testing.T) {
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[{"path":"go.mod","type":"blob","size":10}],"truncated":true}`))
	}))

	entries, err := c.GetTree(context.Background(), "owner", "repo", "main")
	if !errors.Is(err, ErrTreeTruncated) {
		t.Fatalf("err = %v, want ErrTreeTruncated", err)
	}
	if len(entries) != 1 || entries[0].Path != "go.mod" {
		t.Errorf("partial entries should be returned: %+v", entries)
	}
}

func TestDetectManifestsRecursive_TruncatedWithMatches(t *testing.T) {
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[{"path":"sub/go.mod","type":"blob"}],"truncated":true}`))
	}))

	manifests, err := c.DetectManifestsRecursive(context.Background(), "owner", "repo", "main", map[string]string{"go.mod": "go"})
	if err != nil {
		t.Fatalf("DetectManifestsRecursive: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Path != "sub/go.mod" {
		t.Errorf("unexpected manifests: %+v", manifests)
	}
}

func TestDetectManifestsRecursive_TruncatedFallsBackToRoot(t *testing.T) {
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/git/trees/") {
			// Truncated tree with no manifest matches.
			_, _ = w.Write([]byte(`{"tree":[{"path":"README.md","type":"blob"}],"truncated":true}`))
			return
		}
		// Root contents listing finds the manifest.
		_, _ = w.Write([]byte(`[{"name":"go.mod","path":"go.mod","type":"file","size":12}]`))
	}))

	manifests, err := c.DetectManifestsRecursive(context.Background(), "owner", "repo", "main", map[string]string{"go.mod": "go"})
	if err != nil {
		t.Fatalf("DetectManifestsRecursive: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Path != "go.mod" {
		t.Errorf("expected root fallback manifest, got: %+v", manifests)
	}
}

func TestDecodeTokenResponse(t *testing.T) {
	mkResp := func(status int, body string) *http.Response {
		rec := httptest.NewRecorder()
		rec.WriteHeader(status)
		_, _ = rec.WriteString(body)
		return rec.Result()
	}

	if _, err := decodeTokenResponse(mkResp(http.StatusBadGateway, `{}`)); err == nil {
		t.Error("expected error for non-200 status")
	}

	if _, err := decodeTokenResponse(mkResp(http.StatusOK, `{"token_type":"bearer"}`)); err == nil {
		t.Error("expected error for empty access token")
	}

	_, err := decodeTokenResponse(mkResp(http.StatusOK, `{"error":"authorization_pending","error_description":"waiting"}`))
	if err == nil || !strings.Contains(err.Error(), "authorization_pending") {
		t.Errorf("expected authorization_pending error, got %v", err)
	}

	tok, err := decodeTokenResponse(mkResp(http.StatusOK, `{"access_token":"gho_x","token_type":"bearer","scope":"repo"}`))
	if err != nil {
		t.Fatalf("decodeTokenResponse: %v", err)
	}
	if tok.AccessToken != "gho_x" || tok.TokenType != "bearer" || tok.Scope != "repo" {
		t.Errorf("unexpected token: %+v", tok)
	}
}

func TestFetchFileRaw_ReturnsContent(t *testing.T) {
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("raw file body"))
	}))

	content, err := c.FetchFileRaw(context.Background(), "owner", "repo", "main.go", "")
	if err != nil {
		t.Fatalf("FetchFileRaw: %v", err)
	}
	if content != "raw file body" {
		t.Errorf("content = %q, want %q", content, "raw file body")
	}
}

func TestContentClient_RejectsInvalidOwnerRepo(t *testing.T) {
	// The server must never be reached when owner/repo fail validation.
	c := newTestContentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s with invalid owner/repo", r.URL.Path)
	}))

	ctx := context.Background()
	owner, repo := "own/er", "re po" // both invalid

	checks := []struct {
		name string
		call func() error
	}{
		{"ListContents", func() error { _, err := c.ListContents(ctx, owner, repo, "", ""); return err }},
		{"FetchFile", func() error { _, err := c.FetchFile(ctx, owner, repo, "f", ""); return err }},
		{"FetchFileRaw", func() error { _, err := c.FetchFileRaw(ctx, owner, repo, "f", ""); return err }},
		{"SearchCode", func() error { _, err := c.SearchCode(ctx, owner, repo, "q"); return err }},
		{"GetTree", func() error { _, err := c.GetTree(ctx, owner, repo, "main"); return err }},
		{"GetRepoInfo", func() error { _, err := c.GetRepoInfo(ctx, owner, repo); return err }},
		{"ListBranches", func() error { _, err := c.ListBranches(ctx, owner, repo); return err }},
		{"ListTags", func() error { _, err := c.ListTags(ctx, owner, repo); return err }},
		{"GetReadme", func() error { _, err := c.GetReadme(ctx, owner, repo, ""); return err }},
	}

	for _, tc := range checks {
		if err := tc.call(); err == nil {
			t.Errorf("%s should fail validation for invalid owner/repo", tc.name)
		}
	}
}

func TestClientFetch_RejectsInvalidOwnerRepo(t *testing.T) {
	c := NewClient(nil, "", 0)
	for _, ref := range [][2]string{
		{"", "repo"},
		{"owner", ""},
		{"own/er", "repo"},
		{"owner", "re po"},
		{"-owner", "repo"},
	} {
		if _, err := c.Fetch(context.Background(), ref[0], ref[1], false); err == nil {
			t.Errorf("Fetch(%q, %q) should fail validation", ref[0], ref[1])
		}
	}
}
