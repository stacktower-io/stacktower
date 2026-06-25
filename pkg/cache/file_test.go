package cache

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestFileCache(t *testing.T) Cache {
	t.Helper()
	c, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	return c
}

func TestFileCacheRoundTrip(t *testing.T) {
	c := newTestFileCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	data, hit, err := c.Get(ctx, "k")
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if !bytes.Equal(data, []byte("value")) {
		t.Errorf("Get = %q, want %q", data, "value")
	}

	// Expired entries are misses. (TTL <= 0 means "no expiration", so use a
	// tiny positive TTL and let it lapse.)
	if err := c.Set(ctx, "expired", []byte("x"), time.Nanosecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, hit, _ := c.Get(ctx, "expired"); hit {
		t.Error("expired entry should be a miss")
	}
}

// TestFileCacheConcurrentAccess stresses concurrent Get/Set on the same keys.
// Run with -race. Before atomic writes, a Get racing a Set could read a torn
// file, treat it as corrupt, and delete the entry.
func TestFileCacheConcurrentAccess(t *testing.T) {
	c := newTestFileCache(t)
	ctx := context.Background()

	const workers = 8
	const iterations = 50
	payload := bytes.Repeat([]byte("x"), 64*1024) // large enough to make torn writes likely

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", w%2) // contend on 2 keys
			for i := 0; i < iterations; i++ {
				if err := c.Set(ctx, key, payload, time.Minute); err != nil {
					t.Errorf("Set: %v", err)
					return
				}
				data, hit, err := c.Get(ctx, key)
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if hit && len(data) != len(payload) {
					t.Errorf("torn read: got %d bytes, want %d", len(data), len(payload))
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// After the dust settles, both keys must be present and intact.
	for w := 0; w < 2; w++ {
		key := fmt.Sprintf("key-%d", w)
		data, hit, err := c.Get(ctx, key)
		if err != nil || !hit {
			t.Fatalf("Get(%s) after stress: hit=%v err=%v", key, hit, err)
		}
		if !bytes.Equal(data, payload) {
			t.Errorf("Get(%s) corrupted after concurrent writes", key)
		}
	}
}

// TestFileCacheCrashLeavesOldValue simulates a crash mid-write: a leftover
// temp file must not affect reads of the existing entry.
func TestFileCacheCrashLeavesOldValue(t *testing.T) {
	dir := t.TempDir()
	c, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("old"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Simulate an interrupted write: orphaned temp file next to the entry.
	fc := c.(*FileCache)
	path := fc.path("k")
	if err := os.WriteFile(path+".tmp.123", []byte("garbage{"), 0600); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	data, hit, err := c.Get(ctx, "k")
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if string(data) != "old" {
		t.Errorf("Get = %q, want %q", data, "old")
	}
}

func TestFileCacheContextCancelled(t *testing.T) {
	c := newTestFileCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err == nil {
		t.Error("Set with cancelled context should fail")
	}
	if _, _, err := c.Get(ctx, "k"); err == nil {
		t.Error("Get with cancelled context should fail")
	}
}

func TestWriteFileAtomicNoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := writeFileAtomic(path, []byte("data"), 0600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}
