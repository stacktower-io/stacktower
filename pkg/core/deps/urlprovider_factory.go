package deps

import (
	"time"

	"github.com/matzehuels/stacktower/pkg/cache"
)

// NewURLProvider returns the ecosystem URL provider for the given language.
// Returns nil for unsupported languages.
func NewURLProvider(language string, c cache.Cache, cacheTTL time.Duration) URLProvider {
	switch language {
	case "python":
		return NewPyPIURLProvider(c, cacheTTL)
	case "javascript":
		return NewNpmURLProvider(c, cacheTTL)
	case "rust":
		return NewCratesURLProvider(c, cacheTTL)
	case "ruby":
		return NewRubyGemsURLProvider(c, cacheTTL)
	case "go":
		return NewGoProxyURLProvider(c, cacheTTL)
	case "php":
		return NewPackagistURLProvider(c, cacheTTL)
	case "java":
		return NewMavenURLProvider(c, cacheTTL)
	default:
		return nil
	}
}
