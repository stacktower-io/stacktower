package deps

import (
	"fmt"

	"github.com/matzehuels/stacktower/pkg/cache"
)

// Language defines how to resolve dependencies for a programming language.
//
// Each language subpackage (python, rust, javascript, etc.) exports a
// Language value that describes its registry API, manifest formats, and
// how to construct resolvers and parsers.
//
// Language values are typically used by the CLI to dispatch commands like
// "stacktower parse" based on file type or registry name.
type Language struct {
	// Name is the language identifier (e.g., "python", "rust", "javascript").
	Name string

	// DefaultRegistry is the primary registry name for this language
	// (e.g., "pypi" for Python, "crates" for Rust). Used when no registry
	// is explicitly specified.
	DefaultRegistry string

	// DefaultRuntimeVersion is the default runtime version to use when none
	// is specified via CLI or manifest. For example, "3.11" for Python,
	// "20" for Node.js, "1.75" for Rust. Empty string means no default.
	DefaultRuntimeVersion string

	// RegistryAliases maps alternative registry names to canonical names.
	// For example, {"npm": "npm", "npmjs": "npm"}. May be nil or empty.
	RegistryAliases map[string]string

	// ManifestTypes lists supported manifest type identifiers
	// (e.g., ["poetry", "requirements", "pipfile"] for Python). These are
	// the canonical names passed to NewManifest. May be nil or empty.
	ManifestTypes []string

	// ManifestAliases maps filenames to manifest types. For example,
	// {"poetry.lock": "poetry", "requirements.txt": "requirements"}.
	// Used by DetectManifest to match file paths. May be nil or empty.
	ManifestAliases map[string]string

	// NewResolver creates a registry Resolver with the given cache and options.
	// The cache is used for HTTP response caching (use cache.NullCache{} for no caching).
	// Options provides language-specific configuration (e.g., RuntimeVersion for Python).
	// Returns an error if resolver construction fails (e.g., missing
	// configuration). May be nil if the language has no registry support.
	NewResolver func(c cache.Cache, opts Options) (Resolver, error)

	// NewManifest creates a ManifestParser for the given type name and resolver.
	// The name is typically a value from ManifestTypes or ManifestAliases.
	// Returns nil if the type is unrecognized. May be nil if the language
	// has no manifest support. The resolver may be nil for parsers that don't
	// need to fetch additional data.
	NewManifest func(name string, res Resolver) ManifestParser

	// ManifestParsers returns all available ManifestParser implementations
	// for this language. The resolver is passed to each parser and may be nil.
	// Returns nil or an empty slice if the language has no manifest support.
	ManifestParsers func(res Resolver) []ManifestParser

	// NormalizeName transforms a package name to its canonical form.
	// For example, Maven coordinates may use underscores as a filesystem-safe
	// alternative to colons: "com.google.guava_guava" -> "com.google.guava:guava".
	// May be nil if the language doesn't require name normalization.
	NormalizeName func(name string) string
}

// Registry returns a Resolver for the named registry, resolving aliases.
//
// The name is first resolved through RegistryAliases. If it doesn't match
// DefaultRegistry, an error is returned. This method currently only supports
// a single registry per language.
//
// The resolver is created with the given backend and options. Use DefaultCacheTTL
// in opts.CacheTTL if not specified.
//
// Returns an error if the registry name is unknown or if NewResolver fails.
func (l *Language) Registry(backend cache.Cache, name string, opts Options) (Resolver, error) {
	name = l.alias(l.RegistryAliases, name)
	if name != l.DefaultRegistry {
		return nil, fmt.Errorf("unknown registry %q (available: %s)", name, l.DefaultRegistry)
	}
	return l.NewResolver(backend, opts.WithDefaults())
}

// Resolver returns the default registry resolver for this language.
//
// This is a convenience wrapper around NewResolver with the given backend
// and default options.
// Returns an error if NewResolver is nil or fails.
func (l *Language) Resolver(backend cache.Cache, opts Options) (Resolver, error) {
	return l.NewResolver(backend, opts.WithDefaults())
}

// Manifest returns a parser for the named manifest type, resolving aliases.
//
// The name is first resolved through ManifestAliases (e.g., "poetry.lock" -> "poetry"),
// then passed to NewManifest. The resolver may be nil for parsers that don't fetch
// additional data.
//
// Returns (parser, true) if successful, or (nil, false) if:
//   - NewManifest is nil (language has no manifest support)
//   - The manifest type is unrecognized by NewManifest
//
// Safe to call on a zero Language value.
func (l *Language) Manifest(name string, res Resolver) (ManifestParser, bool) {
	if l.NewManifest == nil {
		return nil, false
	}
	p := l.NewManifest(l.alias(l.ManifestAliases, name), res)
	return p, p != nil
}

// HasManifests reports whether this language supports manifest file parsing.
//
// Returns true if NewManifest is non-nil, meaning at least one manifest
// format is supported. This does not guarantee that a specific manifest
// type is recognized—use Manifest to check.
func (l *Language) HasManifests() bool {
	return l.NewManifest != nil
}

func (l *Language) alias(m map[string]string, name string) string {
	if v, ok := m[name]; ok {
		return v
	}
	return name
}
