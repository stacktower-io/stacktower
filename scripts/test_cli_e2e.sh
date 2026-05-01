#!/bin/bash
set -euo pipefail

readonly BIN="./bin/stacktower"
readonly EXAMPLES_DIR="./examples"
readonly OUTPUT_DIR="./output/cli-e2e"

readonly DEFAULT_MAX_DEPTH=10
readonly DEFAULT_MAX_NODES=200
readonly NO_CACHE=${NO_CACHE:-false}

# Render dimensions
readonly RENDER_WIDTH=800
readonly RENDER_HEIGHT=600

# Combined SVG settings
readonly CELL_GAP=20
readonly COMBINED_SCALE=0.5

main() {
    local mode="${1:-all}"

    check_prerequisites "$mode"
    mkdir -p "$OUTPUT_DIR"

    case "$mode" in
        test)
            echo "=== Rendering Test Examples ==="
            run_render_suite "$EXAMPLES_DIR/test"
            ;;
        real)
            echo "=== Rendering Real Examples ==="
            run_render_suite "$EXAMPLES_DIR/real"
            ;;
        parse)
            echo "=== Running Parse Tests ==="
            run_parse_tests
            ;;
        all)
            echo "=== Running All E2E Tests ==="
            run_parse_tests
            run_render_suite "$EXAMPLES_DIR/test"
            run_render_suite "$EXAMPLES_DIR/real"
            ;;
        *)
            echo "Usage: $0 [test|real|parse|all]"
            exit 1
            ;;
    esac

    echo ""
    echo "=== Done ==="
}

check_prerequisites() {
    local mode="$1"

    if [[ ! -f "$BIN" ]]; then
        fail "Binary not found at $BIN\nRun 'make build' first"
    fi

    if ! command -v jq >/dev/null 2>&1; then
        fail "jq is required but not installed"
    fi

    if [[ "$mode" != "parse" ]]; then
        if ! command -v bc >/dev/null 2>&1; then
            fail "bc is required for combined SVG generation"
        fi
        if ! command -v rsvg-convert >/dev/null 2>&1; then
            fail "rsvg-convert is required for PNG/PDF rendering (install librsvg)"
        fi
    fi
}

run_parse_tests() {
    echo ""
    echo "--- Parse Registry (without normalization for render tests) ---"
    mkdir -p "$EXAMPLES_DIR/real"

    # Parse WITHOUT normalization so render tests can show the difference
    # between --normalize=false and --normalize=true
    test_parse_unnormalized python flask
    test_parse_unnormalized python openai
    test_parse_unnormalized rust serde
    test_parse_unnormalized javascript yargs
    test_parse_unnormalized ruby rspec
    test_parse_unnormalized php symfony/console
    test_parse_unnormalized java com.google.guava:guava
    test_parse_unnormalized go github.com/spf13/cobra

    echo ""
    echo "--- Parse Manifests ---"
    test_manifest python poetry.lock
    test_manifest python requirements.txt
    test_manifest rust Cargo.toml
    test_manifest javascript package.json
    test_manifest ruby Gemfile
    test_manifest php composer.json
    test_manifest java pom.xml
    test_manifest go go.mod
}

run_render_suite() {
    local input_dir=$1
    local suite_name
    suite_name=$(basename "$input_dir")

    echo ""
    echo "--- Rendering $suite_name ---"

    for input_file in "$input_dir"/*.json; do
        [[ -f "$input_file" ]] || continue
        local name
        name=$(basename "$input_file" .json)
        render_dag "$name" "$input_file" "$suite_name"
    done
}

render_dag() {
    local name=$1
    local input=$2
    local suite=$3
    local dag_dir="$OUTPUT_DIR/render/$suite/$name"

    echo -n "  $name... "

    mkdir -p "$dag_dir"/{nodelink,tower}

    # ==========================================================================
    # Nodelink visualizations (all formats in single call)
    # ==========================================================================
    
    # Nodelink normalized - all formats at once
    if ! $BIN render "$input" \
        --type nodelink \
        --normalize \
        --format svg,png,pdf \
        -o "$dag_dir/nodelink/normalized" 2>&1 | filter_warnings; then
        fail "nodelink render failed"
    fi

    # Nodelink raw (unnormalized) - SVG only
    if ! $BIN render "$input" \
        --type nodelink \
        --normalize=false \
        -o "$dag_dir/nodelink/raw.svg" 2>&1 | filter_warnings; then
        fail "nodelink raw render failed"
    fi

    # ==========================================================================
    # Tower visualizations (all formats in single call)
    # ==========================================================================

    # Tower simple (unmerged) - all formats at once
    if ! $BIN render "$input" \
        --type tower \
        --normalize \
        --width "$RENDER_WIDTH" \
        --height "$RENDER_HEIGHT" \
        --edges \
        --style simple \
        --merge=false \
        --format svg,png,pdf \
        -o "$dag_dir/tower/simple" 2>&1 | filter_warnings; then
        fail "tower simple render failed"
    fi

    # Tower merged - SVG only (merged is the default)
    if ! $BIN render "$input" \
        --type tower \
        --normalize \
        --width "$RENDER_WIDTH" \
        --height "$RENDER_HEIGHT" \
        --edges \
        --style simple \
        -o "$dag_dir/tower/merged.svg" 2>&1 | filter_warnings; then
        fail "tower merged render failed"
    fi

    # Tower handdrawn - all formats at once (merged by default)
    if ! $BIN render "$input" \
        --type tower \
        --normalize \
        --width "$RENDER_WIDTH" \
        --height "$RENDER_HEIGHT" \
        --style handdrawn \
        --randomize \
        --format svg,png,pdf \
        -o "$dag_dir/tower/handdrawn" 2>&1 | filter_warnings; then
        fail "tower handdrawn render failed"
    fi

    # ==========================================================================
    # Validation
    # ==========================================================================
    
    # Validate SVG outputs
    validate_svg "$dag_dir/nodelink/normalized.svg"
    validate_svg "$dag_dir/nodelink/raw.svg"
    validate_svg "$dag_dir/tower/simple.svg" "rect"
    validate_svg "$dag_dir/tower/merged.svg" "rect"
    validate_svg "$dag_dir/tower/handdrawn.svg" "path"

    # Validate PNG outputs exist
    validate_file "$dag_dir/nodelink/normalized.png"
    validate_file "$dag_dir/tower/simple.png"
    validate_file "$dag_dir/tower/handdrawn.png"

    # Validate PDF outputs exist
    validate_file "$dag_dir/nodelink/normalized.pdf"
    validate_file "$dag_dir/tower/simple.pdf"
    validate_file "$dag_dir/tower/handdrawn.pdf"

    # Create combined view (for backward compatibility)
    create_combined_svg "$dag_dir" "$name"

    echo "OK"
}

# Extract the full viewBox string from SVG
get_svg_viewbox() {
    local file=$1
    grep -oE 'viewBox="[^"]*"' "$file" | head -1 | sed 's/viewBox="//;s/"//'
}

# Extract width and height from SVG viewBox
get_svg_dimensions() {
    local file=$1
    local viewbox
    viewbox=$(get_svg_viewbox "$file")
    
    if [[ -n "$viewbox" ]]; then
        echo "$viewbox" | awk '{print $3, $4}'
    else
        # Fallback to width/height attributes
        local width height
        width=$(grep -oE 'width="[0-9.]+(pt|px)?"' "$file" | head -1 | grep -oE '[0-9.]+')
        height=$(grep -oE 'height="[0-9.]+(pt|px)?"' "$file" | head -1 | grep -oE '[0-9.]+')
        echo "$width $height"
    fi
}

# Extract SVG inner content (strips XML declaration, DOCTYPE, and outer svg tags)
get_svg_content() {
    local file=$1
    # Use awk to extract content between opening <svg...> and closing </svg>
    # Handles multi-line opening tags (common in graphviz output)
    awk '
        /<svg/ { in_svg=1 }
        in_svg && />/ && !content_started { content_started=1; sub(/.*>/, ""); if (length > 0) print; next }
        /<\/svg>/ { sub(/<\/svg>.*/, ""); if (length > 0) print; exit }
        content_started { print }
    ' "$file"
}

# Create a combined SVG with all variants side by side
create_combined_svg() {
    local dag_dir=$1
    local name=$2
    local output="$dag_dir/combined.svg"

    local files=(
        "$dag_dir/nodelink/raw.svg"
        "$dag_dir/nodelink/normalized.svg"
        "$dag_dir/tower/simple.svg"
        "$dag_dir/tower/merged.svg"
        "$dag_dir/tower/handdrawn.svg"
    )
    local labels=("(a) graph" "(b) reduced graph" "(c) stacktower" "(d) merged stacktower" "(e) final stacked tower")

    local x_offset=0
    local total_width=0
    local cells=()

    # Calculate scaled widths for each SVG (scale to match tower height)
    for file in "${files[@]}"; do
        read -r w h <<< "$(get_svg_dimensions "$file")"
        local scale
        scale=$(echo "scale=4; $RENDER_HEIGHT / $h" | bc)
        local scaled_width
        scaled_width=$(echo "$w * $scale" | bc | cut -d. -f1)
        cells+=("$scaled_width")
        total_width=$((total_width + scaled_width + CELL_GAP))
    done
    total_width=$((total_width - CELL_GAP))  # Remove last gap

    local label_height=80
    local total_height=$((RENDER_HEIGHT + label_height))

    # Calculate scaled output dimensions
    local output_width output_height
    output_width=$(echo "$total_width * $COMBINED_SCALE" | bc | cut -d. -f1)
    output_height=$(echo "$total_height * $COMBINED_SCALE" | bc | cut -d. -f1)

    # Start building combined SVG (viewBox keeps full size, width/height scale down)
    cat > "$output" << EOF
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
     viewBox="0 0 $total_width $total_height" width="$output_width" height="$output_height">
  <style>
    .label { font-family: Times, serif; font-size: 24px; fill: #333; }
  </style>
EOF

    # Add each SVG as a nested element
    x_offset=0
    for i in "${!files[@]}"; do
        local file="${files[$i]}"
        local label="${labels[$i]}"
        local cell_width="${cells[$i]}"

        # Get the original viewBox to preserve coordinate space
        local viewbox
        viewbox=$(get_svg_viewbox "$file")

        # Extract SVG content
        local content
        content=$(get_svg_content "$file")

        # Add nested SVG at top (y=0)
        cat >> "$output" << EOF
  <svg x="$x_offset" y="0" width="$cell_width" height="$RENDER_HEIGHT"
       viewBox="$viewbox" preserveAspectRatio="xMidYMid meet">
$content
  </svg>
EOF

        # Add label below the image
        local label_x=$((x_offset + cell_width / 2))
        local label_y=$((RENDER_HEIGHT + 55))
        echo "  <text x=\"$label_x\" y=\"$label_y\" text-anchor=\"middle\" class=\"label\">$label</text>" >> "$output"

        x_offset=$((x_offset + cell_width + CELL_GAP))
    done

    echo "</svg>" >> "$output"
}

validate_svg() {
    local file=$1
    shift

    if [[ ! -s "$file" ]]; then
        fail "output missing or empty: $file"
    fi

    for element in "$@"; do
        if ! grep -q "<$element" "$file"; then
            fail "SVG missing <$element> element: $file"
        fi
    done
}

validate_file() {
    local file=$1
    if [[ ! -s "$file" ]]; then
        fail "output missing or empty: $file"
    fi
}

test_parse() {
    local lang=$1
    local pkg=$2
    local depth=${3:-$DEFAULT_MAX_DEPTH}
    local nodes=${4:-$DEFAULT_MAX_NODES}
    local no_cache=${NO_CACHE:-false}
    # Extract basename and replace colons with underscores (colons not allowed in filenames)
    local basename="${pkg##*/}"
    basename="${basename//:/_}"
    local output="$OUTPUT_DIR/parse/registry/${lang}/${basename}.json"

    mkdir -p "$OUTPUT_DIR/parse/registry/${lang}"

    echo -n "  $lang/$pkg... "

    local cache_flag=""
    if [[ "$no_cache" == "true" ]]; then
        cache_flag="--no-cache"
    fi

    if ! $BIN parse "$lang" "$pkg" \
        --max-depth "$depth" \
        --max-nodes "$nodes" \
        $cache_flag \
        -o "$output" 2>&1 | filter_warnings; then
        fail "parse returned error"
    fi

    validate_json "$output"
    echo "OK"
}

test_parse_unnormalized() {
    local lang=$1
    local pkg=$2
    local depth=${3:-$DEFAULT_MAX_DEPTH}  # Smaller depth for render tests
    local nodes=${4:-$DEFAULT_MAX_NODES} # Smaller graph for render tests
    # Extract basename and replace colons with underscores (colons not allowed in filenames)
    local basename="${pkg##*/}"
    basename="${basename//:/_}"
    local output="$EXAMPLES_DIR/real/${basename}.json"

    echo -n "  $lang/$pkg... "

    # Parse produces raw graphs (normalization happens during layout/render)
    if ! $BIN parse "$lang" "$pkg" \
        --max-depth "$depth" \
        --max-nodes "$nodes" \
        -o "$output" 2>&1 | filter_warnings; then
        fail "parse returned error"
    fi

    validate_json "$output"
    echo "OK"
}

test_manifest() {
    local lang=$1
    local file=$2
    local depth=${3:-$DEFAULT_MAX_DEPTH}
    local nodes=${4:-$DEFAULT_MAX_NODES}
    local manifest_path="$EXAMPLES_DIR/manifest/$file"
    local output="$OUTPUT_DIR/parse/manifest/${lang}/${file%.*}.json"

    echo -n "  $lang/$file... "

    if [[ ! -f "$manifest_path" ]]; then
        fail "manifest not found: $manifest_path"
    fi

    mkdir -p "$OUTPUT_DIR/parse/manifest/${lang}"

    if ! $BIN parse "$lang" "$manifest_path" \
        --max-depth "$depth" \
        --max-nodes "$nodes" \
        -o "$output" 2>&1 | filter_warnings; then
        fail "manifest parse returned error"
    fi

    validate_json "$output"
    echo "OK"
}

validate_json() {
    local file=$1

    if [[ ! -f "$file" ]]; then
        fail "output file not created"
    fi

    if ! jq -e '.nodes | length > 0' "$file" >/dev/null 2>&1; then
        fail "invalid JSON or no nodes"
    fi
}

filter_warnings() {
    grep -v "^WARN:" || true
}

fail() {
    echo -e "FAIL: $*" >&2
    exit 1
}

main "$@"
