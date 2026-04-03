# Contributing to Stacktower

Thanks for your interest in contributing!

## Getting Started

```bash
git clone https://github.com/matzehuels/stacktower.git
cd stacktower
make install-tools  # Install golangci-lint, goimports, govulncheck
make check          # Run all CI checks locally
```

## Development Workflow

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/amazing-feature`)
3. Make your changes
4. Run checks: `make check`
5. Commit with [Conventional Commits](https://www.conventionalcommits.org/) format:
   - `feat: add new feature`
   - `fix: resolve bug`
   - `docs: update readme`
   - `refactor: restructure code`
   - `test: add tests`
   - `ci: update workflows`
6. Push and open a Pull Request

## Code Style

- Run `make fmt` before committing
- Run `make lint` to check for issues
- Keep changes focused and minimal

## Running Tests

```bash
make test       # Unit tests
make e2e        # End-to-end tests
make cover      # Tests with coverage
```

## Architecture

Stacktower follows a clean layered architecture. See the [pkg.go.dev documentation](https://pkg.go.dev/github.com/matzehuels/stacktower) for detailed API docs.

```
internal/cli/                  # Command-line interface
pkg/core/dag/                  # Core DAG data structure
├── transform/                 # Graph normalization (transitive reduction, subdivision)
└── perm/                      # PQ-tree and permutation algorithms
pkg/core/deps/                 # Dependency resolution from registries
├── python/                    # Python: PyPI + poetry.lock + requirements.txt
├── rust/                      # Rust: crates.io + Cargo.toml
├── javascript/                # JavaScript: npm + package.json
├── ruby/                      # Ruby: RubyGems + Gemfile
├── php/                       # PHP: Packagist + composer.json
├── java/                      # Java: Maven Central + pom.xml
├── golang/                    # Go: Go Module Proxy + go.mod
└── metadata/                  # GitHub/GitLab enrichment providers
pkg/integrations/              # Registry API clients (npm, pypi, crates, etc.)
pkg/core/render/tower/         # Tower visualization
├── ordering/                  # Barycentric and optimal ordering algorithms
├── layout/                    # Block position computation
├── sink/                      # Output formats (SVG, JSON, PDF, PNG)
└── styles/                    # Visual styles (handdrawn, simple)
pkg/graph/                     # JSON import/export
pkg/pipeline/                  # Parse → layout → render orchestration
pkg/security/                  # Vulnerability scanning and license analysis
pkg/cache/                     # Caching interfaces and implementations
```

## Adding a New Language

1. **Create an integration client** in `pkg/integrations/<registry>/client.go`:

```go
type Client struct {
    *integrations.Client
    baseURL string
}

func NewClient(cacheTTL time.Duration) (*Client, error) {
    cache, err := integrations.NewCache(cacheTTL)
    if err != nil {
        return nil, err
    }
    return &Client{
        Client:  integrations.NewClient(cache, nil),
        baseURL: "https://registry.example.com",
    }, nil
}

func (c *Client) FetchPackage(ctx context.Context, name string, refresh bool) (*PackageInfo, error) {
    // Implement caching and fetching
}
```

2. **Create a language definition** in `pkg/deps/<lang>/<lang>.go`:

```go
var Language = &deps.Language{
    Name:            "mylang",
    DefaultRegistry: "myregistry",
    RegistryAliases: map[string]string{"alias": "myregistry"},
    ManifestTypes:   []string{"my.lock"},
    ManifestAliases: map[string]string{"my.lock": "mylock"},
    NewResolver:     newResolver,
    NewManifest:     newManifest,
    ManifestParsers: manifestParsers,
}
```

3. **Register in CLI** in `internal/cli/parse.go`

See [`pkg/core/deps`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/deps) for detailed documentation.

## Adding a Manifest Parser

Implement the `ManifestParser` interface:

```go
type MyLockParser struct{}

func (p *MyLockParser) Type() string              { return "my.lock" }
func (p *MyLockParser) IncludesTransitive() bool  { return true }
func (p *MyLockParser) Supports(name string) bool { return name == "my.lock" }

func (p *MyLockParser) Parse(path string, opts deps.Options) (*deps.ManifestResult, error) {
    g := dag.New(nil)
    // ... populate nodes and edges
    return &deps.ManifestResult{Graph: g, Type: p.Type(), IncludesTransitive: true}, nil
}
```

## Adding a New Output Format

Output formats are "sinks" in `pkg/core/render/tower/sink/`. Each sink takes a `layout.Layout` and renders it to bytes.

1. **Create a sink file** in `pkg/render/tower/sink/<format>.go`:

```go
func RenderMyFormat(l layout.Layout, opts ...MyFormatOption) ([]byte, error) {
    // Access layout data:
    // - l.FrameWidth, l.FrameHeight: canvas dimensions
    // - l.Blocks: map[string]Block with position data
    // - l.RowOrders: node ordering per row
    return []byte("..."), nil
}
```

2. **Register in CLI** in `internal/cli/render.go`

See [`pkg/core/render/tower/sink`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/render/tower/sink) for existing implementations.

## Questions?

Open an [issue](https://github.com/matzehuels/stacktower/issues) — we're happy to help!

