package pipeline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/stacktower-io/stacktower/pkg/cache"
	"github.com/stacktower-io/stacktower/pkg/security"
)

type failingSetCache struct {
	setErr error
}

func (c *failingSetCache) Get(context.Context, string) ([]byte, bool, error) { return nil, false, nil }
func (c *failingSetCache) Set(context.Context, string, []byte, time.Duration) error {
	return c.setErr
}
func (c *failingSetCache) Delete(context.Context, string) error { return nil }
func (c *failingSetCache) Close() error                         { return nil }

var _ cache.Cache = (*failingSetCache)(nil)

// recordingCache stores Set calls in memory so tests can assert on cache writes.
type recordingCache struct {
	data map[string][]byte
}

func newRecordingCache() *recordingCache {
	return &recordingCache{data: make(map[string][]byte)}
}

func (c *recordingCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	d, ok := c.data[key]
	return d, ok, nil
}

func (c *recordingCache) Set(_ context.Context, key string, data []byte, _ time.Duration) error {
	c.data[key] = data
	return nil
}

func (c *recordingCache) Delete(_ context.Context, key string) error {
	delete(c.data, key)
	return nil
}

func (c *recordingCache) Close() error { return nil }

var _ cache.Cache = (*recordingCache)(nil)

type stubScanner struct {
	err   error
	calls int
}

func (s *stubScanner) Scan(_ context.Context, _ []security.Dependency) (*security.Report, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &security.Report{}, nil
}

const minimalPoetryLock = `[[package]]
name = "requests"
version = "2.31.0"
description = "Python HTTP for Humans."
category = "main"
optional = false
python-versions = ">=3.7"
`

func securityScanOpts() Options {
	return Options{
		Language:         "python",
		Manifest:         minimalPoetryLock,
		ManifestFilename: "poetry.lock",
		SkipEnrich:       true,
		SecurityScan:     true,
	}
}

func TestParseWithCache_ScanFailureNotCached(t *testing.T) {
	c := newRecordingCache()
	scanner := &stubScanner{err: errors.New("osv unreachable")}
	runner := NewRunnerWithScanner(c, nil, log.New(&bytes.Buffer{}), scanner)

	result, err := runner.ParseWithCacheInfo(context.Background(), securityScanOpts())
	if err != nil {
		t.Fatalf("parse should succeed despite scan failure: %v", err)
	}
	if result.CacheHit {
		t.Fatal("first run should not be a cache hit")
	}
	if scanner.calls != 1 {
		t.Fatalf("scanner calls = %d, want 1", scanner.calls)
	}
	if len(c.data) != 0 {
		t.Fatalf("graph must not be cached after scan failure, got %d cache entries", len(c.data))
	}

	// Second run must retry the scan (no poisoned cache entry).
	scanner.err = nil
	result, err = runner.ParseWithCacheInfo(context.Background(), securityScanOpts())
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if result.CacheHit {
		t.Fatal("second run should re-parse, not hit cache")
	}
	if scanner.calls != 2 {
		t.Fatalf("scanner calls = %d, want 2 (scan must be retried)", scanner.calls)
	}
	if len(c.data) == 0 {
		t.Fatal("graph should be cached after a successful scan")
	}
}

func TestParseWithCache_ScannerMissingNotCached(t *testing.T) {
	c := newRecordingCache()
	runner := NewRunner(c, nil, log.New(&bytes.Buffer{}))

	_, err := runner.ParseWithCacheInfo(context.Background(), securityScanOpts())
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(c.data) != 0 {
		t.Fatalf("graph must not be cached when scan was requested but no scanner is configured, got %d entries", len(c.data))
	}
}

func TestSetCacheWithWarningLogsFailure(t *testing.T) {
	var out bytes.Buffer
	logger := log.NewWithOptions(&out, log.Options{Level: log.DebugLevel})
	runner := NewRunner(&failingSetCache{setErr: errors.New("disk full")}, nil, logger)

	runner.setCacheWithWarning(context.Background(), "graph:key", []byte("data"), time.Minute, "parse")

	got := out.String()
	if !strings.Contains(got, "cache write failed") {
		t.Fatalf("expected warning log, got %q", got)
	}
	if !strings.Contains(got, "stage=parse") {
		t.Fatalf("expected stage field, got %q", got)
	}
}
