#!/bin/bash
set -euo pipefail

readonly BIN="./bin/stacktower"
readonly OUTPUT_DIR="./blogpost/plots/showcase"
readonly CACHE_DIR="./blogpost/cache"

readonly MAX_DEPTH=10
readonly MAX_NODES=100

# Desktop dimensions
readonly WIDTH=900
readonly HEIGHT=500

# Mobile dimensions (portrait-friendly)
readonly MOBILE_WIDTH=800
readonly MOBILE_HEIGHT=900

# Package lists (name:description)
PYTHON_PKGS="openai:Clean OpenAI client
requests:Venerable HTTP client
pydantic:Data validation
fastapi:Modern web framework
typer:CLI framework"

RUST_PKGS="serde:Serialization
ureq:Simple HTTP client
hyper:HTTP implementation
diesel:ORM and query builder
rayon:Parallel iterators"

JS_PKGS="yup:Schema validation
mongoose:MongoDB ODM
knex:SQL query builder
ioredis:Redis client
pino:Fast logging"

main() {
    check_prerequisites
    mkdir -p "$OUTPUT_DIR"/{python,rust,javascript}
    mkdir -p "$CACHE_DIR"

    echo "=== Generating Showcase Diagrams ==="
    
    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
        echo "Warning: GITHUB_TOKEN not set, metadata enrichment disabled"
    else
        echo "GitHub metadata enrichment enabled"
    fi

    generate_lang "python" "$PYTHON_PKGS"
    generate_lang "rust" "$RUST_PKGS"
    generate_lang "javascript" "$JS_PKGS"

    echo ""
    echo "=== Done ==="
    echo "Output: $OUTPUT_DIR"
}

check_prerequisites() {
    if [[ ! -f "$BIN" ]]; then
        echo "Binary not found at $BIN. Run 'make build' first" >&2
        exit 1
    fi
}

generate_lang() {
    local lang=$1
    local pkgs=$2
    
    echo ""
    echo "--- $lang ---"
    
    while IFS=: read -r pkg desc; do
        [[ -z "$pkg" ]] && continue
        generate_package "$lang" "$pkg" "$desc"
    done <<< "$pkgs"
}

generate_package() {
    local lang=$1
    local pkg=$2
    local desc=$3
    local cache_file="$CACHE_DIR/${lang}_${pkg}.json"
    local output_file="$OUTPUT_DIR/$lang/$pkg.svg"
    local output_file_mobile="$OUTPUT_DIR/$lang/${pkg}_mobile.svg"

    echo -n "  $pkg ($desc)... "

    # Parse if not cached
    if [[ ! -f "$cache_file" ]]; then
        if ! $BIN parse "$lang" "$pkg" \
            --enrich \
            --max-depth "$MAX_DEPTH" \
            --max-nodes "$MAX_NODES" \
            -o "$cache_file" 2>&1 | grep -v "^WARN:" >&2; then
            echo "FAIL (parse)"
            return 1
        fi
    fi

    local nodes edges
    nodes=$(jq '.nodes | length' "$cache_file")
    edges=$(jq '.edges | length' "$cache_file")

    # Render desktop version
    if ! $BIN render "$cache_file" \
        -t tower \
        --style handdrawn \
        --width "$WIDTH" \
        --height "$HEIGHT" \
        --ordering optimal \
        --merge \
        --randomize \
        --popups=false \
        -o "$output_file" 2>&1 | grep -v "^WARN:" >&2; then
        echo "FAIL (render desktop)"
        return 1
    fi

    # Render mobile version (portrait-friendly dimensions)
    if ! $BIN render "$cache_file" \
        -t tower \
        --style handdrawn \
        --width "$MOBILE_WIDTH" \
        --height "$MOBILE_HEIGHT" \
        --ordering optimal \
        --merge \
        --popups=false \
        --randomize \
        -o "$output_file_mobile" 2>&1 | grep -v "^WARN:" >&2; then
        echo "FAIL (render mobile)"
        return 1
    fi

    echo "OK ($nodes nodes, $edges edges)"
}

main "$@"

