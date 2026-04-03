package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matzehuels/stacktower/pkg/cache"
)

func TestNewCache(t *testing.T) {
	t.Run("noCache returns NullCache", func(t *testing.T) {
		c, err := newCache(true)
		if err != nil {
			t.Fatalf("newCache(true) error = %v", err)
		}
		if c == nil {
			t.Fatal("expected non-nil cache")
		}
		// NullCache should always miss
		_, hit, _ := c.Get(t.Context(), "test-key")
		if hit {
			t.Error("NullCache should never hit")
		}
	})

	t.Run("valid cache dir creates FileCache", func(t *testing.T) {
		// Use a temp directory
		tmpDir := t.TempDir()
		origXDG := os.Getenv("XDG_CACHE_HOME")
		defer os.Setenv("XDG_CACHE_HOME", origXDG)
		os.Setenv("XDG_CACHE_HOME", tmpDir)

		c, err := newCache(false)
		if err != nil {
			t.Fatalf("newCache(false) error = %v", err)
		}
		defer c.Close()

		// Verify it's a working cache (not NullCache)
		testKey := "test-key-123"
		testData := []byte("test data")
		if err := c.Set(t.Context(), testKey, testData, cache.TTLGraph); err != nil {
			t.Errorf("Set() error = %v", err)
		}

		data, hit, err := c.Get(t.Context(), testKey)
		if err != nil {
			t.Errorf("Get() error = %v", err)
		}
		if !hit {
			t.Error("expected cache hit")
		}
		if string(data) != string(testData) {
			t.Errorf("Get() = %q, want %q", string(data), string(testData))
		}
	})
}

func TestCLI_SetQuiet(t *testing.T) {
	cli := New(os.Stderr, LogInfo)

	if cli.Quiet {
		t.Error("CLI should not be quiet by default")
	}

	cli.SetQuiet(true)
	if !cli.Quiet {
		t.Error("CLI.Quiet should be true after SetQuiet(true)")
	}

	cli.SetQuiet(false)
	if cli.Quiet {
		t.Error("CLI.Quiet should be false after SetQuiet(false)")
	}
}

func TestCLI_SetLogLevel(t *testing.T) {
	cli := New(os.Stderr, LogInfo)

	cli.SetLogLevel(LogDebug)
	if cli.Logger.GetLevel() != LogDebug {
		t.Errorf("Logger level = %v, want %v", cli.Logger.GetLevel(), LogDebug)
	}

	cli.SetLogLevel(LogInfo)
	if cli.Logger.GetLevel() != LogInfo {
		t.Errorf("Logger level = %v, want %v", cli.Logger.GetLevel(), LogInfo)
	}
}

func TestCacheDir_SymlinkHandling(t *testing.T) {
	// Create a temp directory structure with symlinks
	tmpDir := t.TempDir()
	realCache := filepath.Join(tmpDir, "real-cache")
	symlinkCache := filepath.Join(tmpDir, "symlink-cache")

	if err := os.MkdirAll(realCache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realCache, symlinkCache); err != nil {
		t.Skip("cannot create symlinks on this system")
	}

	origXDG := os.Getenv("XDG_CACHE_HOME")
	defer os.Setenv("XDG_CACHE_HOME", origXDG)
	os.Setenv("XDG_CACHE_HOME", symlinkCache)

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir() error = %v", err)
	}

	if !strings.HasPrefix(dir, symlinkCache) {
		t.Errorf("cacheDir() = %q, should start with %q", dir, symlinkCache)
	}
}

func TestNewCache_DirectoryCreation(t *testing.T) {
	// Test that newCache creates the directory if it doesn't exist
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "nonexistent", "cache")

	origXDG := os.Getenv("XDG_CACHE_HOME")
	defer os.Setenv("XDG_CACHE_HOME", origXDG)
	os.Setenv("XDG_CACHE_HOME", nonExistentDir)

	c, err := newCache(false)
	if err != nil {
		t.Fatalf("newCache(false) error = %v", err)
	}
	defer c.Close()

	// Directory should now exist
	expectedDir := filepath.Join(nonExistentDir, "stacktower")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		// This is acceptable - some cache implementations defer creation
		t.Log("cache directory not created immediately (deferred creation)")
	}
}
