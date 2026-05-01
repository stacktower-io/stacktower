package cache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FileCache implements a file-based cache for CLI usage.
// Cache entries are stored as files in a directory with metadata (expiration).
type FileCache struct {
	dir string
}

// NewFileCache creates a file-based cache in the given directory.
// The directory will be created if it doesn't exist.
func NewFileCache(dir string) (Cache, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &FileCache{dir: dir}, nil
}

// cacheEntry wraps cached data with metadata.
type cacheEntry struct {
	Data      []byte    `json:"data"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Get retrieves a value from the cache.
func (c *FileCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	path := c.path(key)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		// Invalid cache entry - treat as miss
		_ = os.Remove(path)
		return nil, false, nil
	}

	// Check expiration
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		_ = os.Remove(path)
		return nil, false, nil
	}

	return entry.Data, true, nil
}

// Set stores a value in the cache.
func (c *FileCache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	entry := cacheEntry{
		Data: data,
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}

	entryData, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	path := c.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	return os.WriteFile(path, entryData, 0600)
}

// Delete removes a value from the cache.
func (c *FileCache) Delete(ctx context.Context, key string) error {
	path := c.path(key)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Close does nothing for file cache.
func (c *FileCache) Close() error {
	return nil
}

// path converts a cache key to a file path.
// Uses a simple hash-based directory structure to avoid too many files in one dir.
func (c *FileCache) path(key string) string {
	hash := Hash([]byte(key))
	// Use first 2 chars as subdirectory for distribution
	subdir := hash[:2]
	filename := hash[2:] + ".json"
	return filepath.Join(c.dir, subdir, filename)
}

// Ensure FileCache implements Cache.
var _ Cache = (*FileCache)(nil)
