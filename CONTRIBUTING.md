# Contributing to Stacktower

Thank you for your interest in contributing! This guide covers the most common contribution paths: adding new languages, manifest parsers, and output formats.

## Getting Started

```bash
git clone https://github.com/stacktower-io/stacktower.git
cd stacktower
make install-tools
make check
```

`make check` runs formatting, linting, tests, and vulnerability checks. All four must pass before submitting a PR.

## Project Structure

```
cmd/stacktower/       CLI entrypoint
internal/cli/         CLI commands, flags, output formatting
internal/cli/ui/      Terminal UI components (spinners, tables, styles)
pkg/core/dag/         DAG data structure and crossing algorithms
pkg/core/deps/        Dependency resolution per language ecosystem
pkg/core/render/      Layout and rendering (tower, node-link)
pkg/pipeline/         Parse → layout → render pipeline
pkg/integrations/     External API clients (GitHub, OSV, registries)
pkg/security/         Vulnerability scanning
pkg/sbom/             SBOM generation (CycloneDX, SPDX)
pkg/cache/            Caching layer
pkg/graph/            Wire types for graph I/O
```

The boundary between `pkg/` (stable library API) and `internal/` (CLI-only) is intentional. Library consumers import from `pkg/`; CLI-specific behavior stays in `internal/cli/`.

## Adding a New Language Ecosystem

Each language lives in its own package under `pkg/core/deps/<language>/` and implements the `deps.Language` struct. Use an existing ecosystem (e.g., `pkg/core/deps/python/`) as a reference.

### Steps

1. **Create the package** at `pkg/core/deps/<language>/`.

2. **Define the `Language` variable** with registry info, manifest types, and resolver/parser factories:
   - `Name` — lowercase identifier (e.g., `"python"`, `"rust"`)
   - `DefaultRegistry` — primary registry name
   - `ManifestFilenames` — map of filename to manifest type
   - `NewResolver` — factory for the registry resolver
   - `NewManifest` — factory for manifest parsers

3. **Implement a resolver** that fetches package metadata from the registry and returns a `deps.Package` with versions and dependencies.

4. **Implement manifest parsers** for each supported manifest/lock file format.

5. **Register the language** in `pkg/core/deps/languages/languages.go` by adding it to the `All` slice.

6. **Add tests** — at minimum, unit tests for the resolver and each manifest parser.

7. **Add example manifests** in `examples/manifest/` for the new ecosystem.

8. **Update the README** — add the language to the supported languages list and the manifest file table.

## Adding a New Manifest Parser

To add support for a new manifest file within an existing language:

1. Add a new manifest type in the language's package (e.g., `pkg/core/deps/python/`).
2. Register the filename in the language's `ManifestFilenames` map.
3. Implement the parser — it receives raw file bytes and returns a dependency graph.
4. Add unit tests with representative fixture files.
5. Add an example manifest to `examples/manifest/`.

## Adding a New Output Format

Rendering output formats are handled in `pkg/core/render/`. To add a new format:

1. Implement a sink in the appropriate render package (e.g., `pkg/core/render/tower/sink/`).
2. Register the format in `pkg/pipeline/formats.go`.
3. Wire it into the CLI in `internal/cli/render.go`.

## Code Style

- **Formatting**: `gofmt` + `goimports` with local prefix `github.com/stacktower-io/stacktower`. Run `make fmt` before committing.
- **Linting**: `golangci-lint` with the config in `.golangci.yml`. Run `make lint`.
- **Comments**: Avoid obvious narration. Comments should explain _why_, not _what_.
- **Errors**: Use `CLIError` with `Kind`, `Message`, and `Hint` for user-facing errors in `internal/cli/`. Library code in `pkg/` returns plain errors.
- **Tests**: Table-driven tests preferred. Use `_test.go` files adjacent to the code they test.

## Commit Messages

This project follows [Conventional Commits](https://www.conventionalcommits.org/). PR titles are validated automatically.

```
feat: add Swift ecosystem support
fix(python): handle empty requirements.txt
docs: update manifest file table
test(rust): add Cargo.lock parser edge cases
```

Valid types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`, `build`, `revert`.

## Pull Requests

- One logical change per PR.
- Include tests for new functionality.
- Update the README if your change affects CLI behavior, flags, or supported ecosystems.
- All CI checks must pass (format, lint, test, vulncheck).

## Reporting Issues

Use [GitHub Issues](https://github.com/stacktower-io/stacktower/issues) for bugs and feature requests. For security vulnerabilities, see [SECURITY.md](SECURITY.md).
