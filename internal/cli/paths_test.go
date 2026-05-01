package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheDir(t *testing.T) {
	oldXdg := os.Getenv("XDG_CACHE_HOME")
	os.Unsetenv("XDG_CACHE_HOME")
	defer func() {
		if oldXdg != "" {
			os.Setenv("XDG_CACHE_HOME", oldXdg)
		}
	}()

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cache", appName)
	if dir != expected {
		t.Errorf("cacheDir() = %q, want %q", dir, expected)
	}
}

func TestCacheDirXDG(t *testing.T) {
	customCache := "/tmp/custom-cache"
	oldXdg := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", customCache)
	defer func() {
		if oldXdg != "" {
			os.Setenv("XDG_CACHE_HOME", oldXdg)
		} else {
			os.Unsetenv("XDG_CACHE_HOME")
		}
	}()

	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir() error: %v", err)
	}

	expected := filepath.Join(customCache, appName)
	if dir != expected {
		t.Errorf("cacheDir() with XDG_CACHE_HOME = %q, want %q", dir, expected)
	}
}
