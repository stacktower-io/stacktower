# Stacktower

[![CI](https://github.com/matzehuels/stacktower/actions/workflows/ci.yml/badge.svg)](https://github.com/matzehuels/stacktower/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/matzehuels/stacktower/graph/badge.svg)](https://codecov.io/gh/matzehuels/stacktower)
[![Go Report Card](https://goreportcard.com/badge/github.com/matzehuels/stacktower)](https://goreportcard.com/report/github.com/matzehuels/stacktower)
[![Go Reference](https://pkg.go.dev/badge/github.com/matzehuels/stacktower.svg)](https://pkg.go.dev/github.com/matzehuels/stacktower)
[![Release](https://img.shields.io/github/v/release/matzehuels/stacktower)](https://github.com/matzehuels/stacktower/releases)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Inspired by [XKCD #2347](https://xkcd.com/2347/), Stacktower renders dependency graphs as **physical towers** where blocks rest on what they depend on. Your application sits at the top, supported by libraries below—all the way down to that one critical package maintained by _some dude in Nebraska_.

<p align="center">
  <img src="blogpost/plots/showcase/python/fastapi.svg" alt="FastAPI dependency tower" width="600">
</p>

---

<p align="center">
  <a href="https://app.stacktower.io"><strong>🚀 Try the Web App</strong></a> · <a href="https://www.stacktower.io"><strong>📖 Read the Story</strong></a>
</p>

---

> ⚠️ **Note:** The ordering algorithms are still experimental and may not produce nicely stacked towers for projects with a large number of dependencies. Results can vary. We're actively working on improvements.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install matzehuels/tap/stacktower
```

### Go

```bash
go install github.com/matzehuels/stacktower@latest
```

### From Source

```bash
git clone https://github.com/matzehuels/stacktower.git
cd stacktower
go build -o bin/stacktower ./cmd/stacktower
```

## Quick Start

```bash
# Render the included Flask example (XKCD-style tower is the default)
stacktower render examples/real/flask.json -o flask.svg
```

## Usage

Stacktower works in two stages: **parse** dependency data from package registries or manifest files, then **render** visualizations.

### Parsing Dependencies

The `parse` command auto-detects whether you're providing a package name or a manifest file:

```bash
stacktower parse <language> <package-or-file> [flags]
```

**Supported languages:** `python`, `rust`, `javascript`, `ruby`, `php`, `java`, `go`

#### From Package Registries

```bash
stacktower parse python fastapi -o fastapi.json                  # PyPI
stacktower parse rust serde -o serde.json                        # crates.io
stacktower parse javascript yargs -o yargs.json                  # npm
stacktower parse ruby rails -o rails.json                        # RubyGems
stacktower parse php monolog/monolog -o monolog.json             # Packagist
stacktower parse java com.google.guava:guava -o guava.json       # Maven Central
stacktower parse go github.com/gin-gonic/gin -o gin.json         # Go Module Proxy
```

Use `package@version` to pin a specific version. Without it, the latest version is resolved:

```bash
stacktower parse python fastapi@0.104.1 -o fastapi.json
stacktower parse rust serde@1.0.195 -o serde.json
stacktower parse javascript @angular/core@17.0.0 -o angular.json  # scoped packages work too
```

#### From Manifest Files (Auto-Detected)

```bash
stacktower parse python examples/manifest/poetry.lock -o deps.json
stacktower parse python examples/manifest/requirements.txt -o deps.json
stacktower parse rust examples/manifest/Cargo.toml -o deps.json
stacktower parse javascript examples/manifest/package.json -o deps.json
stacktower parse ruby examples/manifest/Gemfile -o deps.json
stacktower parse php examples/manifest/composer.json -o deps.json
stacktower parse java examples/manifest/pom.xml -o deps.json
stacktower parse go examples/manifest/go.mod -o deps.json
```

When the argument exists on disk or matches a known manifest filename, Stacktower automatically parses it as a manifest.

The project name (root node) is auto-detected from the manifest or a sibling file:

- **Cargo.toml**: `[package].name`
- **go.mod**: `module` directive
- **package.json**: `name` field
- **composer.json**: `name` field
- **pom.xml**: `groupId:artifactId`
- **poetry.lock / requirements.txt**: `pyproject.toml` (sibling)
- **Gemfile**: `*.gemspec` (sibling)

Use `--name` to override the auto-detected name:

```bash
stacktower parse python requirements.txt --name="my-project" -o deps.json
stacktower parse ruby Gemfile -n my-rails-app -o deps.json
```

#### Metadata Enrichment

By default, Stacktower enriches packages with GitHub metadata (stars, maintainers, last commit) for richer visualizations. Set `GITHUB_TOKEN` to enable this:

```bash
export GITHUB_TOKEN=your_token
stacktower parse python fastapi -o fastapi.json

# Skip enrichment if you don't have a token
stacktower parse python fastapi --enrich=false -o fastapi.json
```

#### Vulnerability Scanning

Stacktower can scan your dependency graph for known vulnerabilities using the [OSV.dev](https://osv.dev/) database. Add the `--security-scan` flag during parsing to annotate every package with its highest vulnerability severity:

```bash
# Parse with vulnerability scanning
stacktower parse python fastapi --security-scan -o fastapi.json

# Parse a manifest with scanning
stacktower parse javascript package.json --security-scan -o deps.json
```

When `--security-scan` is enabled, Stacktower queries OSV.dev in a single batch request and writes severity data (`critical`, `high`, `medium`, `low`) into each node's metadata. The scanned graph is cached separately from non-scanned graphs, so subsequent runs are instant.

During rendering, vulnerable packages are automatically colour-coded by severity (red for critical, orange for high, amber for medium, yellow for low). Both tower and node-link visualizations support this. Use `--show-vulns=false` to suppress the colours while keeping the data in the graph:

```bash
# Render with vulnerability colours (default when data exists)
stacktower render fastapi.json -o fastapi.svg

# Render without vulnerability colours
stacktower render fastapi.json --show-vulns=false -o fastapi.svg
```

The `--show-vulns` flag is available on the `render`, `layout`, and `visualize` commands and defaults to `true`.

### Resolving Dependencies

The `resolve` command is a lightweight alternative to `parse`, designed for quickly testing dependency resolution on local manifest and lock files. It auto-detects the language from the filename and prints a human-readable dependency tree instead of graph JSON:

```bash
stacktower resolve <manifest-file>
stacktower resolve <language> <package[@version]>
```

#### From Manifest / Lock Files (Auto-Detected)

```bash
stacktower resolve poetry.lock                          # Python (Poetry)
stacktower resolve uv.lock                              # Python (uv)
stacktower resolve requirements.txt                     # Python (pip)
stacktower resolve package-lock.json                    # JavaScript (npm)
stacktower resolve Cargo.lock                           # Rust
stacktower resolve Gemfile.lock                         # Ruby
stacktower resolve composer.lock                        # PHP
stacktower resolve go.mod                               # Go
```

No `--language` flag needed — the filename is enough.

#### From Package Registries

```bash
stacktower resolve python fastapi
stacktower resolve rust serde@1.0.195
stacktower resolve javascript yargs
```

#### Output

By default, `resolve` prints an indented dependency tree to stdout:

```
fastapi 0.104.1
  starlette 0.27.0
    anyio 4.0.0
      sniffio 1.3.0
  pydantic 2.5.2
    pydantic-core 2.14.5
    typing-extensions 4.8.0

Resolved 7 packages (max depth: 3, direct: 2)
```

Use `-o` to save the resolution as JSON (same format as `parse`), suitable for piping into `stacktower render`:

```bash
stacktower resolve Cargo.lock -o deps.json
stacktower render deps.json -o deps.svg
```

### Listing Versions

The `list` command shows all available versions of a package from its registry, sorted semantically with the latest stable version highlighted:

```bash
stacktower list <language> <package> [flags]
```

```bash
stacktower list python fastapi
stacktower list rust serde
stacktower list javascript react
stacktower list go github.com/gin-gonic/gin
```

Output (truncated to 20 most recent by default):

```
  fastapi  python · 277 versions
  latest   0.129.2

  0.129.1   0.129.0   0.128.8   0.128.7   0.128.6   0.128.5   0.128.4
  0.128.3   0.128.2   0.128.1   0.128.0   0.127.1   0.127.0   0.126.0
  0.125.0   0.124.4   0.124.3   0.124.2   0.124.1   0.124.0

  … 256 older versions not shown (use --all to show all)
```

Use `--all` to see every version:

```bash
stacktower list python fastapi --all
```

Pre-release versions (alpha, beta, rc, dev) are shown dimmed and sorted after stable releases.

### Rendering

The `render` command generates visualizations from parsed JSON graphs:

```bash
stacktower render <file> [flags]
```

This is a shortcut that combines `layout` and `visualize` in one step. For more control, you can run them separately:

```bash
# Two-step workflow with intermediate layout
stacktower layout examples/real/flask.json -o flask.layout.json
stacktower visualize flask.layout.json -o flask.svg
```

#### Visualization Types

```bash
# Hand-drawn XKCD-style tower (default)
stacktower render examples/real/flask.json -o flask.svg

# Disable hand-drawn effects for a cleaner look
stacktower render examples/real/serde.json --style simple --randomize=false --popups=false -o serde.svg

# Traditional node-link diagram (uses Graphviz DOT)
stacktower render examples/real/yargs.json -t nodelink -o yargs.svg
```

#### Output Formats

```bash
# SVG output (default)
stacktower render examples/real/flask.json -o flask.svg

# JSON layout export (for external tools or re-rendering)
stacktower render examples/real/flask.json -f json -o flask.json

# PDF output
stacktower render examples/real/flask.json -f pdf -o flask.pdf

# PNG output (2x scale by default)
stacktower render examples/real/flask.json -f png -o flask.png

# Multiple formats at once (outputs flask.svg, flask.json, flask.pdf)
stacktower render examples/real/flask.json -f svg,json,pdf -o flask
```

Output path behavior:

- **No `-o`**: Derives from input (`input.json` → `input.<format>`)
- **Single format**: Uses exact path (`-o out.svg` → `out.svg`)
- **Multiple formats**: Strips extension, adds format (`-o out -f svg,json` → `out.svg`, `out.json`)

> **Note:** PDF and PNG output requires [librsvg](https://wiki.gnome.org/Projects/LibRsvg):
>
> - macOS: `brew install librsvg`
> - Linux: `apt install librsvg2-bin`

### Included Examples

The repository ships with pre-parsed graphs and manifest files so you can experiment immediately:

```bash
# Real packages with full metadata (XKCD-style by default)
stacktower render examples/real/flask.json -o flask.svg
stacktower render examples/real/serde.json -o serde.svg
stacktower render examples/real/yargs.json -o yargs.svg

# With Nebraska guy maintainer ranking
stacktower render examples/real/flask.json --nebraska -o flask.svg

# Synthetic test cases for layout algorithm testing
stacktower render examples/test/diamond.json -o diamond.svg
stacktower render examples/test/crossing.json -o crossing.svg
```

> **Note:** For accurate Nebraska rankings, parse with `--contributors` to fetch maintainer data:
> ```bash
> stacktower parse python flask --contributors -o flask.json
> stacktower render flask.json --nebraska -o flask.svg
> ```

#### Example Manifest & Lock Files

The `examples/manifest/` directory contains sample manifest and lock files for every supported ecosystem, useful for testing `resolve` and `parse` locally:

| File | Language | Type |
| --- | --- | --- |
| `poetry.lock` | Python | Lock file |
| `uv.lock` | Python | Lock file |
| `pyproject.toml` | Python | Manifest |
| `requirements.txt` | Python | Manifest |
| `package-lock.json` | JavaScript | Lock file |
| `package.json` | JavaScript | Manifest |
| `Cargo.lock` | Rust | Lock file |
| `Cargo.toml` | Rust | Manifest |
| `Gemfile.lock` | Ruby | Lock file |
| `Gemfile` | Ruby | Manifest |
| `composer.lock` | PHP | Lock file |
| `composer.json` | PHP | Manifest |
| `go.mod` | Go | Manifest |
| `pom.xml` | Java | Manifest |

```bash
# Quickly test dependency resolution on any of these
stacktower resolve examples/manifest/poetry.lock
stacktower resolve examples/manifest/Cargo.lock
stacktower resolve examples/manifest/package-lock.json
stacktower resolve examples/manifest/Gemfile.lock
stacktower resolve examples/manifest/composer.lock
```

## Command Reference

Stacktower provides a suite of commands organized around a parse → render workflow. Each command is documented below with all available options and reproducible examples.

### Global Options

These flags apply to all commands:

| Flag              | Description                                                |
| ----------------- | ---------------------------------------------------------- |
| `-v`, `--verbose` | Enable debug logging (search space info, timing details)   |
| `-q`, `--quiet`   | Suppress non-essential output (success messages, stats)    |
| `-h`, `--help`    | Show help for any command                                  |
| `--version`       | Show version information                                   |

---

### `stacktower parse`

Parse dependency graphs from package registries or local manifest files.

```bash
stacktower parse <language> <package-or-file> [flags]
stacktower parse <manifest-file>                       # Auto-detect language
stacktower parse github [owner/repo]                   # Parse from GitHub
```

#### Parse Options

| Flag                    | Description                                                                          |
| ----------------------- | ------------------------------------------------------------------------------------ |
| `-o`, `--output`        | Output file (stdout if empty)                                                        |
| `-n`, `--name`          | Project name for manifest parsing (auto-detected if not set)                         |
| `--max-depth N`         | Maximum dependency depth (default: 10, max: 100)                                     |
| `--max-nodes N`         | Maximum packages to fetch (default: 5000, max: 50000)                                |
| `--workers N`           | Concurrent fetch workers (default: 20)                                               |
| `--enrich`              | Enrich with GitHub metadata — stars, maintainers (default: true)                     |
| `--contributors`        | Fetch GitHub contributors for Nebraska rankings (slower API calls)                   |
| `--security-scan`       | Scan dependencies for known vulnerabilities via OSV.dev                              |
| `--dependency-scope`    | Dependency scope: `prod_only` (default) or `all` (includes dev dependencies)         |
| `--include-prerelease`  | Include prerelease versions (alpha/beta/rc/dev) in resolution                        |
| `--runtime-version`     | Target runtime version for marker evaluation (e.g., `3.11` for Python)               |
| `--no-cache`            | Disable caching                                                                      |

#### Parse Examples

```bash
# Parse from package registries
stacktower parse python requests -o requests.json
stacktower parse python fastapi@0.104.1 -o fastapi.json      # Pin specific version
stacktower parse rust serde -o serde.json
stacktower parse javascript @angular/core@17.0.0 -o angular.json
stacktower parse go github.com/gin-gonic/gin -o gin.json

# Parse from manifest files (language auto-detected)
stacktower parse poetry.lock -o deps.json
stacktower parse package-lock.json -o deps.json
stacktower parse Cargo.lock -o deps.json

# Parse with explicit language
stacktower parse python requirements.txt -o deps.json
stacktower parse javascript package.json --name="my-app" -o deps.json

# Include dev dependencies
stacktower parse python poetry.lock --dependency-scope all -o deps.json

# Target specific Python version for environment markers
stacktower parse python fastapi --runtime-version 3.9 -o fastapi-py39.json

# Scan for vulnerabilities during parsing
stacktower parse python django --security-scan -o django.json

# Full metadata including contributors (slower)
stacktower parse python flask --contributors -o flask.json
```

#### Parse from GitHub

Parse dependencies directly from a GitHub repository with interactive selection:

```bash
stacktower parse github                              # Full interactive flow
stacktower parse github owner/repo                   # Select ref + manifest
stacktower parse github owner/repo --ref v2.0.0     # Parse at specific tag
stacktower parse github owner/repo --ref main       # Explicit branch
stacktower parse github owner/repo --timeout 10m    # Custom timeout
```

| Flag        | Description                                         |
| ----------- | --------------------------------------------------- |
| `--ref`     | Git ref (branch, tag, or commit SHA)                |
| `--timeout` | Timeout for GitHub operations (default: 5m)         |

> **Private repos:** To parse private repositories, first install the GitHub App with
> `stacktower github install` and grant access to the repos you need.

---

### `stacktower resolve`

Lightweight alternative to `parse` for quick dependency resolution testing. Auto-detects language from manifest filenames and outputs a human-readable dependency tree.

```bash
stacktower resolve <manifest-file>
stacktower resolve <language> <package[@version]>
```

#### Resolve Options

| Flag                   | Description                                                                  |
| ---------------------- | ---------------------------------------------------------------------------- |
| `-o`, `--output`       | Output file for JSON (stdout shows tree by default)                          |
| `-n`, `--name`         | Project name (for manifest parsing)                                          |
| `--max-depth N`        | Maximum dependency depth (default: 10)                                       |
| `--max-nodes N`        | Maximum packages to fetch (default: 5000)                                    |
| `--enrich`             | Enrich with GitHub metadata (off by default, unlike `parse`)                 |
| `--dependency-scope`   | Dependency scope: `prod_only` (default) or `all`                             |
| `--include-prerelease` | Include prerelease versions in resolution                                    |
| `--runtime-version`    | Target runtime version for marker evaluation                                 |
| `--no-cache`           | Disable caching                                                              |

#### Resolve Examples

```bash
# Auto-detect language from filename
stacktower resolve poetry.lock
stacktower resolve Cargo.lock
stacktower resolve package-lock.json
stacktower resolve go.mod

# Resolve from registry
stacktower resolve python fastapi
stacktower resolve rust serde@1.0.195

# Save resolution as JSON
stacktower resolve poetry.lock -o deps.json

# Include dev dependencies
stacktower resolve poetry.lock --dependency-scope all

# Target specific runtime
stacktower resolve python fastapi --runtime-version 3.8
```

#### resolve vs parse

| | `resolve` | `parse` |
| --- | --- | --- |
| Language detection | Auto-detected from filename | Must specify language subcommand |
| Default output | Human-readable tree | Graph JSON |
| Metadata enrichment | Off (opt-in with `--enrich`) | On by default (`--enrich=false` to skip) |
| Best for | Local testing, inspecting deps | Rendering pipeline, CI |

---

### `stacktower list`

List all available versions of a package from its registry, sorted semantically with the latest stable version highlighted.

```bash
stacktower list <language> <package> [flags]
```

#### List Options

| Flag                   | Description                                                           |
| ---------------------- | --------------------------------------------------------------------- |
| `--all`                | Show all versions (default: newest 20)                                |
| `--runtime-version`    | Filter versions compatible with a specific runtime (e.g., `3.8`)      |
| `--supported-runtimes` | Display runtime constraint for each version                           |
| `--no-cache`           | Bypass cached version data                                            |

#### List Examples

```bash
# List recent versions
stacktower list python fastapi
stacktower list rust serde
stacktower list javascript react

# Show all versions
stacktower list python django --all

# Filter by Python version compatibility
stacktower list python fastapi --runtime-version 3.8

# Show runtime requirements for each version
stacktower list python fastapi --supported-runtimes
```

**Output example:**

```
  fastapi  python · 277 versions
  latest   0.129.2

  0.129.1   0.129.0   0.128.8   0.128.7   0.128.6   0.128.5   0.128.4
  0.128.3   0.128.2   0.128.1   0.128.0   0.127.1   0.127.0   0.126.0
  0.125.0   0.124.4   0.124.3   0.124.2   0.124.1   0.124.0

  … 256 older versions not shown (use --all to show all)
```

---

### `stacktower render`

Generate visualizations from parsed JSON graphs. This is a shortcut that combines `layout` and `visualize` in one step.

```bash
stacktower render <graph.json> [flags]
```

#### Render Options

| Flag               | Description                                                              |
| ------------------ | ------------------------------------------------------------------------ |
| `-o`, `--output`   | Output file or base path for multiple formats                            |
| `-t`, `--type`     | Visualization type: `tower` (default), `nodelink`                        |
| `-f`, `--format`   | Output format(s): `svg` (default), `json`, `pdf`, `png` (comma-separated)|
| `--normalize`      | Apply graph normalization (default: true)                                |
| `--show-vulns`     | Show vulnerability severity colours (default: true)                      |
| `--show-licenses`  | Show license compliance indicators — copyleft/unknown borders (default: true) |
| `--flags-on-top`   | Render security flags on top of all blocks (default: true)               |
| `--no-cache`       | Disable caching                                                          |

#### Tower-Specific Options

| Flag                              | Description                                                           |
| --------------------------------- | --------------------------------------------------------------------- |
| `--width N`                       | Frame width in pixels (default: 800)                                  |
| `--height N`                      | Frame height in pixels (default: 600)                                 |
| `--style handdrawn\|simple`       | Visual style (default: handdrawn)                                     |
| `--randomize`                     | Vary block widths to visualize load-bearing structure (default: true) |
| `--merge`                         | Merge subdivider blocks into continuous towers (default: true)        |
| `--popups`                        | Enable hover popups with package metadata (default: true)             |
| `--nebraska`                      | Show "Nebraska guy" maintainer ranking panel                          |
| `--edges`                         | Show dependency edges as dashed lines                                 |
| `--ordering optimal\|barycentric` | Crossing minimization algorithm (default: optimal)                    |
| `--ordering-timeout N`            | Timeout for optimal search in seconds (default: 60)                   |

#### Render Examples

```bash
# Basic tower rendering (XKCD-style)
stacktower render flask.json -o flask.svg

# Clean style without hand-drawn effects
stacktower render serde.json --style simple --randomize=false --popups=false -o serde.svg

# Node-link diagram (uses Graphviz DOT layout)
stacktower render yargs.json -t nodelink -o yargs.svg

# With Nebraska maintainer rankings
stacktower render flask.json --nebraska -o flask.svg

# Multiple output formats
stacktower render flask.json -f svg,pdf,png -o flask

# Large graph with faster ordering
stacktower render big-project.json --ordering barycentric -o big.svg

# Custom dimensions
stacktower render flask.json --width 1200 --height 900 -o flask-large.svg

# Show dependency edges
stacktower render flask.json --edges -o flask-edges.svg

# Disable vulnerability colours
stacktower render scanned.json --show-vulns=false -o clean.svg
```

---

### `stacktower layout`

Compute visualization layout from a dependency graph. Outputs a layout JSON that can be rendered separately with `visualize`.

```bash
stacktower layout <graph.json> [flags]
```

#### Layout Options

| Flag                              | Description                                                           |
| --------------------------------- | --------------------------------------------------------------------- |
| `-o`, `--output`                  | Output file (default: `<input>.layout.json`)                          |
| `-t`, `--type`                    | Visualization type: `tower` (default), `nodelink`                     |
| `--normalize`                     | Apply graph normalization (default: true)                             |
| `--width N`                       | Frame width in pixels (default: 800)                                  |
| `--height N`                      | Frame height in pixels (default: 600)                                 |
| `--style`                         | Visual style: `handdrawn` (default), `simple`                         |
| `--ordering`                      | Ordering algorithm: `optimal` (default), `barycentric`                |
| `--ordering-timeout N`            | Timeout for optimal search in seconds (default: 60)                   |
| `--randomize`                     | Randomize block widths (tower, default: true)                         |
| `--merge`                         | Merge subdivider blocks (tower, default: true)                        |
| `--nebraska`                      | Show Nebraska maintainer ranking (tower)                              |
| `--show-vulns`                    | Show vulnerability colours (default: true)                            |
| `--show-licenses`                 | Show license indicators (default: true)                               |
| `--flags-on-top`                  | Render security flags on top of all blocks (default: true)            |
| `--no-cache`                      | Disable caching                                                       |

#### Layout Example

```bash
# Compute layout separately
stacktower layout flask.json -o flask.layout.json
stacktower visualize flask.layout.json -o flask.svg
```

---

### `stacktower visualize`

Render visualization from a computed layout (produced by `layout`).

```bash
stacktower visualize <layout.json> [flags]
```

#### Visualize Options

| Flag               | Description                                                              |
| ------------------ | ------------------------------------------------------------------------ |
| `-o`, `--output`   | Output file or base path for multiple formats                            |
| `-f`, `--format`   | Output format(s): `svg` (default), `pdf`, `png` (comma-separated)        |
| `--style`          | Visual style: `handdrawn` (default), `simple`                            |
| `--edges`          | Show dependency edges (tower)                                            |
| `--popups`         | Show hover popups with metadata (default: true)                          |
| `--show-vulns`     | Show vulnerability colours (default: true)                               |
| `--show-licenses`  | Show license indicators (default: true)                                  |
| `--flags-on-top`   | Render security flags on top of all blocks (default: true)               |
| `--no-cache`       | Disable caching                                                          |

#### Visualize Example

```bash
stacktower visualize flask.layout.json -o flask.svg
stacktower visualize flask.layout.json -f pdf -o flask.pdf
```

---

### `stacktower cache`

Manage the local HTTP response cache (`~/.cache/stacktower/`).

```bash
stacktower cache clear    # Delete all cached entries
stacktower cache path     # Print cache directory path
stacktower cache stats    # Show entry count, size, and age
```

#### Cache Examples

```bash
# Check cache statistics
stacktower cache stats

# Clear the cache before a fresh run
stacktower cache clear
stacktower parse python requests -o requests.json

# Get cache path for scripting
CACHE_DIR=$(stacktower cache path)
```

---

### `stacktower github`

GitHub authentication and app installation commands.

```bash
stacktower github login     # Authenticate with GitHub (device flow)
stacktower github install   # Install/configure the GitHub App for repo access
stacktower github whoami    # Show current session and app installation status
stacktower github logout    # Remove stored credentials
```

#### GitHub Examples

```bash
# Login (opens browser for device authorization)
stacktower github login

# Install the GitHub App to grant access to your repositories
# Opens browser to configure which repos Stacktower can access
stacktower github install

# Verify session and check app installation
stacktower github whoami

# Parse from GitHub (auto-prompts login if needed)
stacktower parse github owner/repo -o deps.json
```

> **Note:** To access private repositories, you must install the Stacktower GitHub App
> and grant it access to the specific repos you want to analyze. Run `stacktower github install`
> to configure repository access.

---

### `stacktower info`

Display supported languages, registries, and manifest filenames.

```bash
stacktower info
```

**Output:**

```
stacktower v1.0.0 (abc1234, 2024-01-15)

Supported Languages
  python     registry: pypi.org
    manifests: poetry.lock, pyproject.toml, requirements.txt, uv.lock
  rust       registry: crates.io
    manifests: Cargo.lock, Cargo.toml
  javascript registry: registry.npmjs.org
    manifests: package-lock.json, package.json
  ...

Docs: https://www.stacktower.io
```

---

### `stacktower completion`

Generate shell completion scripts.

```bash
stacktower completion bash
stacktower completion zsh
stacktower completion fish
stacktower completion powershell
```

#### Completion Setup

```bash
# Bash (Linux)
stacktower completion bash > /etc/bash_completion.d/stacktower

# Bash (macOS with Homebrew)
stacktower completion bash > $(brew --prefix)/etc/bash_completion.d/stacktower

# Zsh
stacktower completion zsh > "${fpath[1]}/_stacktower"

# Fish
stacktower completion fish > ~/.config/fish/completions/stacktower.fish
```

---

### `stacktower pqtree`

Debug tool for visualizing PQ-tree constraints used in the ordering algorithm.

```bash
stacktower pqtree --labels A,B,C,D -o tree.svg
stacktower pqtree --labels A,B,C,D -o tree.svg 0,1      # Constraint: A,B adjacent
stacktower pqtree --labels A,B,C,D -o tree.svg 0,1 2,3  # Multiple constraints
```

## JSON Format

The render layer accepts a simple JSON format, making it easy to visualize **any** directed graph—not just package dependencies. You can hand-craft graphs for component diagrams, callgraphs, or pipe output from other tools.

### Minimal Example

```json
{
  "nodes": [{ "id": "app" }, { "id": "lib-a" }, { "id": "lib-b" }],
  "edges": [
    { "from": "app", "to": "lib-a" },
    { "from": "lib-a", "to": "lib-b" }
  ]
}
```

### Required Fields

| Field          | Type   | Description                                 |
| -------------- | ------ | ------------------------------------------- |
| `nodes[].id`   | string | Unique node identifier (displayed as label) |
| `edges[].from` | string | Source node ID                              |
| `edges[].to`   | string | Target node ID                              |

### Optional Fields

| Field                    | Type   | Description                                                        |
| ------------------------ | ------ | ------------------------------------------------------------------ |
| `nodes[].row`            | int    | Pre-assigned layer (computed automatically if omitted)             |
| `nodes[].kind`           | string | Internal use: `"subdivider"` or `"auxiliary"`                      |
| `nodes[].vuln_severity`  | string | Max vulnerability severity: `critical`, `high`, `medium`, or `low` |
| `nodes[].meta`           | object | Freeform metadata for display features                             |

### Recognized `meta` Keys

These keys are read by specific render flags. All are optional—missing keys simply disable the corresponding feature.

| Key                 | Type          | Used By                                    |
| ------------------- | ------------- | ------------------------------------------ |
| `repo_url`          | string        | Clickable blocks, `--popups`, `--nebraska` |
| `repo_stars`        | int           | `--popups`                                 |
| `repo_owner`        | string        | `--nebraska`                               |
| `repo_maintainers`  | []string      | `--nebraska`                               |
| `repo_last_commit`  | string (date) | `--popups`, brittle detection              |
| `repo_last_release` | string (date) | `--popups`                                 |
| `repo_archived`     | bool          | `--popups`, brittle detection              |
| `summary`           | string        | `--popups` (fallback: `description`)       |
| `vuln_severity`     | string        | `--show-vulns` (severity colour coding)    |

### Piping

When `parse` detects that stdout is piped, it emits clean JSON with no chrome — making it composable with other tools:

```bash
# Pipe directly into render
stacktower parse python flask | stacktower render -o flask.svg

# Pipe into jq for analysis
stacktower parse python requests -o - | jq '.nodes | length'

# Combine with security scan
stacktower parse python django --security-scan | stacktower render --show-vulns -o django.svg
```

Progress indicators and summary messages always go to stderr, so they never corrupt piped JSON.

### Terminal Output

A typical `parse` run looks like this:

```
⠙ Resolving python/flask...  [42/5000]
  starlette  jinja2  markupsafe  click

✓ Resolved flask (python)
  42 packages · 67 edges · depth 5 · fresh · 2.3s
  → flask.json
Render: stacktower render flask.json
```

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `rate limited: too many requests` | GitHub/PyPI API rate limit exceeded | Set `GITHUB_TOKEN`; use `--no-cache` sparingly |
| `librsvg` / `rsvg-convert` errors | Missing system dependency for PDF/PNG | Install librsvg: `brew install librsvg` (macOS), `apt install librsvg2-bin` (Linux) |
| Very slow for large graphs | Graph exceeds default limits | Lower `--max-nodes` or `--max-depth`; use `--ordering barycentric` for faster layout |
| `context deadline exceeded` | Ordering search timeout | Increase `--ordering-timeout` or switch to `--ordering barycentric` |
| No colours in output | Terminal doesn't support ANSI or `NO_COLOR` is set | Unset `NO_COLOR`; check terminal supports 256 colours |

## Exit Codes

Stacktower uses stable exit codes for automation and CI:

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Runtime/system failure (network, registry/API, render/pipeline errors) |
| `2` | Invalid usage or input (unsupported language, invalid package/manifest arguments) |
| `130` | Interrupted (`Ctrl+C` / termination signal) |

## How It Works

1. **Parse** — Fetch package metadata from registries or local manifest files
2. **Scan** _(optional)_ — Query OSV.dev for known vulnerabilities and annotate nodes by severity
3. **Reduce** — Remove transitive edges to show only direct dependencies
4. **Layer** — Assign each package to a row based on its depth
5. **Order** — Minimize edge crossings using branch-and-bound with PQ-tree pruning
6. **Layout** — Compute block widths proportional to downstream dependents
7. **Render** — Generate output in SVG, JSON, PDF, or PNG format, with vulnerability colours when available

The ordering step is where the magic happens. Stacktower uses an optimal search algorithm that guarantees minimum crossings for small-to-medium graphs. For larger graphs, it gracefully falls back after a configurable timeout.

## Environment Variables

| Variable              | Description                                                      |
| --------------------- | ---------------------------------------------------------------- |
| `GITHUB_TOKEN`        | GitHub API token for metadata enrichment                         |
| `XDG_CACHE_HOME`      | Override default cache directory (`~/.cache`)                    |
| `NO_COLOR`            | Disable colour output (see https://no-color.org)                 |

## Using as a Library

Stacktower can be used as a Go library for programmatic graph visualization.

```go
import (
    "github.com/matzehuels/stacktower/pkg/core/dag"
    "github.com/matzehuels/stacktower/pkg/core/dag/transform"
    "github.com/matzehuels/stacktower/pkg/core/render/tower/layout"
    "github.com/matzehuels/stacktower/pkg/core/render/tower/sink"
)

// Build a graph
g := dag.New(nil)
g.AddNode(dag.Node{ID: "app", Row: 0})
g.AddNode(dag.Node{ID: "lib", Row: 1})
g.AddEdge(dag.Edge{From: "app", To: "lib"})

// Normalize and render
_, _ = transform.Normalize(g)
l := layout.Build(g, 800, 600)
svg := sink.RenderSVG(l, sink.WithGraph(g), sink.WithPopups())
```

📚 **[Full API documentation on pkg.go.dev](https://pkg.go.dev/github.com/matzehuels/stacktower)**

Key packages:

- [`pkg/core/dag`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/dag) — DAG data structure and crossing algorithms
- [`pkg/core/dag/transform`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/dag/transform) — Graph normalization pipeline
- [`pkg/core/render/tower`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/render/tower) — Layout, ordering, and rendering
- [`pkg/core/deps`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/core/deps) — Dependency resolution from registries
- [`pkg/pipeline`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/pipeline) — Complete parse → layout → render pipeline
- [`pkg/security`](https://pkg.go.dev/github.com/matzehuels/stacktower/pkg/security) — Vulnerability scanning via OSV.dev

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on adding new languages, manifest parsers, or output formats.

## Development

```bash
make install-tools  # Install required tools (golangci-lint, goimports, govulncheck)
make check          # Run all CI checks locally (fmt, lint, test, vuln)
make build          # Build binary to bin/stacktower
```

| Command         | Description                                |
| --------------- | ------------------------------------------ |
| `make check`    | Format, lint, test, vulncheck (same as CI) |
| `make fmt`      | Format code with gofmt and goimports       |
| `make lint`     | Run golangci-lint                          |
| `make test`     | Run tests with race detector               |
| `make cover`    | Run tests with coverage report             |
| `make vuln`     | Check for known vulnerabilities            |
| `make e2e`      | Run end-to-end tests                       |
| `make snapshot` | Build release locally (no publish)         |

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).

## Learn More

- 📖 **[stacktower.io](https://www.stacktower.io)** — Interactive examples and the full story behind tower visualizations
- 🐛 **[Issues](https://github.com/matzehuels/stacktower/issues)** — Bug reports and feature requests

## License

Apache-2.0
