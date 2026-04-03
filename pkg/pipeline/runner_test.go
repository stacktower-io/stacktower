package pipeline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/matzehuels/stacktower/pkg/cache"
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
