package cache

import (
	"context"
	"strings"
	"time"

	"github.com/matzehuels/stacktower/pkg/observability"
)

// InstrumentedCache wraps a Cache and emits observability hooks on Get/Set operations.
type InstrumentedCache struct {
	inner Cache
}

// NewInstrumentedCache wraps a cache to emit CacheHooks on every operation.
func NewInstrumentedCache(inner Cache) Cache {
	if inner == nil {
		return NewNullCache()
	}
	return &InstrumentedCache{inner: inner}
}

func (c *InstrumentedCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	data, hit, err := c.inner.Get(ctx, key)
	if err == nil {
		kt := keyType(key)
		if hit {
			observability.Cache().OnCacheHit(ctx, kt)
		} else {
			observability.Cache().OnCacheMiss(ctx, kt)
		}
	}
	return data, hit, err
}

func (c *InstrumentedCache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	err := c.inner.Set(ctx, key, data, ttl)
	if err == nil {
		observability.Cache().OnCacheSet(ctx, keyType(key), len(data))
	}
	return err
}

func (c *InstrumentedCache) Delete(ctx context.Context, key string) error {
	return c.inner.Delete(ctx, key)
}

func (c *InstrumentedCache) Close() error {
	return c.inner.Close()
}

// Dir returns the underlying directory for FileCache, empty string otherwise.
func (c *InstrumentedCache) Dir() string {
	if fc, ok := c.inner.(*FileCache); ok {
		return fc.dir
	}
	return ""
}

func keyType(key string) string {
	if i := strings.Index(key, ":"); i > 0 {
		return key[:i]
	}
	return "unknown"
}

var _ Cache = (*InstrumentedCache)(nil)
