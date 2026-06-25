package pipeline

import (
	"fmt"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

func TestValidateFormat(t *testing.T) {
	tests := []struct {
		format  string
		wantErr bool
	}{
		{"svg", false},
		{"png", false},
		{"pdf", false},
		{"json", false},
		{"invalid", true},
		{"SVG", true}, // case-sensitive
		{"", true},
	}

	for _, tt := range tests {
		err := ValidateFormat(tt.format)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateFormat(%q) error = %v, wantErr %v", tt.format, err, tt.wantErr)
		}
	}
}

func TestValidateFormats(t *testing.T) {
	if err := ValidateFormats([]string{"svg", "png"}); err != nil {
		t.Errorf("Valid formats should pass: %v", err)
	}

	if err := ValidateFormats([]string{"svg", "invalid"}); err == nil {
		t.Error("Invalid format should fail")
	}

	// Empty slice is valid
	if err := ValidateFormats(nil); err != nil {
		t.Errorf("Empty formats should pass: %v", err)
	}
}

func TestValidateStyle(t *testing.T) {
	tests := []struct {
		style   string
		wantErr bool
	}{
		{"simple", false},
		{"handdrawn", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		err := ValidateStyle(tt.style)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateStyle(%q) error = %v, wantErr %v", tt.style, err, tt.wantErr)
		}
	}
}

func TestValidateVizType(t *testing.T) {
	tests := []struct {
		vizType string
		wantErr bool
	}{
		{"tower", false},
		{"nodelink", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		err := ValidateVizType(tt.vizType)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateVizType(%q) error = %v, wantErr %v", tt.vizType, err, tt.wantErr)
		}
	}
}

func TestOptionsDefaults(t *testing.T) {
	opts := Options{
		Language: "python",
		Package:  "requests",
	}

	if err := opts.ValidateForParse(); err != nil {
		t.Errorf("Valid options should pass: %v", err)
	}

	// Check defaults were set
	if opts.MaxDepth != DefaultMaxDepth {
		t.Errorf("MaxDepth should be %d, got %d", DefaultMaxDepth, opts.MaxDepth)
	}
	if opts.MaxNodes != DefaultMaxNodes {
		t.Errorf("MaxNodes should be %d, got %d", DefaultMaxNodes, opts.MaxNodes)
	}
	if opts.DependencyScope != deps.DependencyScopeProdOnly {
		t.Errorf("DependencyScope should default to %q, got %q", deps.DependencyScopeProdOnly, opts.DependencyScope)
	}
}

func TestOptionsValidateForParse(t *testing.T) {
	// Missing language
	opts := Options{Package: "requests"}
	if err := opts.ValidateForParse(); err == nil {
		t.Error("Missing language should fail")
	}

	// Missing package and manifest
	opts = Options{Language: "python"}
	if err := opts.ValidateForParse(); err == nil {
		t.Error("Missing package/manifest should fail")
	}

	// Manifest without filename
	opts = Options{Language: "python", Manifest: "content"}
	if err := opts.ValidateForParse(); err == nil {
		t.Error("Manifest without filename should fail")
	}

	// Valid with manifest
	opts = Options{Language: "python", Manifest: "content", ManifestFilename: "poetry.lock"}
	if err := opts.ValidateForParse(); err != nil {
		t.Errorf("Valid manifest options should pass: %v", err)
	}

	// Manifest filename must be a basename because in-memory manifests are
	// written to a temporary directory before parser execution.
	opts = Options{Language: "python", Manifest: "content", ManifestFilename: "../poetry.lock"}
	if err := opts.ValidateForParse(); err == nil {
		t.Error("Manifest filename with path traversal should fail")
	}

	// Invalid dependency scope
	opts = Options{Language: "python", Package: "requests", DependencyScope: "invalid"}
	if err := opts.ValidateForParse(); err == nil {
		t.Error("Invalid dependency_scope should fail")
	}
}

func TestOptionsIsTower(t *testing.T) {
	opts := Options{}
	if !opts.IsTower() {
		t.Error("Empty VizType should be tower")
	}

	opts.VizType = "tower"
	if !opts.IsTower() {
		t.Error("tower VizType should be tower")
	}

	opts.VizType = "nodelink"
	if opts.IsTower() {
		t.Error("nodelink VizType should not be tower")
	}
}

func TestOptionsIsNodelink(t *testing.T) {
	opts := Options{}
	if opts.IsNodelink() {
		t.Error("Empty VizType should not be nodelink")
	}

	opts.VizType = "nodelink"
	if !opts.IsNodelink() {
		t.Error("nodelink VizType should be nodelink")
	}
}

func TestOptionsShouldEnrich(t *testing.T) {
	opts := Options{}
	if !opts.ShouldEnrich() {
		t.Error("Default should enrich")
	}

	opts.SkipEnrich = true
	if opts.ShouldEnrich() {
		t.Error("SkipEnrich=true should not enrich")
	}
}

func TestOptionsNeedsOptimalOrderer(t *testing.T) {
	opts := Options{}
	if !opts.NeedsOptimalOrderer() {
		t.Error("Empty ordering should need optimal orderer")
	}

	opts.Ordering = "optimal"
	if !opts.NeedsOptimalOrderer() {
		t.Error("optimal ordering should need optimal orderer")
	}

	opts.Ordering = "barycentric"
	if opts.NeedsOptimalOrderer() {
		t.Error("barycentric ordering should not need optimal orderer")
	}
}

func TestOptionsValidateAndSetDefaultsIdempotent(t *testing.T) {
	opts := Options{
		Language: "python",
		Package:  "requests",
	}

	// First call
	if err := opts.ValidateAndSetDefaults(); err != nil {
		t.Fatalf("First validation failed: %v", err)
	}

	originalMaxDepth := opts.MaxDepth
	originalVizType := opts.VizType
	originalStyle := opts.Style

	// Second call should be idempotent
	if err := opts.ValidateAndSetDefaults(); err != nil {
		t.Fatalf("Second validation failed: %v", err)
	}

	if opts.MaxDepth != originalMaxDepth {
		t.Error("MaxDepth changed on second call")
	}
	if opts.VizType != originalVizType {
		t.Error("VizType changed on second call")
	}
	if opts.Style != originalStyle {
		t.Error("Style changed on second call")
	}
}

func TestSetLayoutDefaults(t *testing.T) {
	opts := Options{}
	opts.SetLayoutDefaults()

	if opts.VizType != DefaultVizType {
		t.Errorf("VizType should be %s, got %s", DefaultVizType, opts.VizType)
	}
	if opts.Width != DefaultWidth {
		t.Errorf("Width should be %f, got %f", DefaultWidth, opts.Width)
	}
	if opts.Height != DefaultHeight {
		t.Errorf("Height should be %f, got %f", DefaultHeight, opts.Height)
	}
	if opts.Seed != DefaultSeed {
		t.Errorf("Seed should be %d, got %d", DefaultSeed, opts.Seed)
	}
}

func TestSetRenderDefaults(t *testing.T) {
	opts := Options{}
	opts.SetRenderDefaults()

	if len(opts.Formats) != 1 || opts.Formats[0] != FormatSVG {
		t.Errorf("Formats should be [svg], got %v", opts.Formats)
	}
	if opts.Style != DefaultStyle {
		t.Errorf("Style should be %s, got %s", DefaultStyle, opts.Style)
	}
}

func TestAdaptForGraph_SmallGraph(t *testing.T) {
	g := dag.New(nil)
	for i := range 5 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: i})
	}

	opts := Options{}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	if opts.Width != DefaultWidth {
		t.Errorf("Small graph should keep default width, got %f", opts.Width)
	}
	if opts.Height != DefaultHeight {
		t.Errorf("Small graph should keep default height, got %f", opts.Height)
	}
}

func TestAdaptForGraph_WideGraph(t *testing.T) {
	g := dag.New(nil)
	for i := range 30 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: 0})
	}
	g.AddNode(dag.Node{ID: "root", Row: 1})

	opts := Options{}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	if opts.Width <= DefaultWidth {
		t.Errorf("Wide graph should scale width above default, got %f", opts.Width)
	}
}

func TestAdaptForGraph_TallGraph(t *testing.T) {
	g := dag.New(nil)
	for i := range 15 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: i})
	}

	opts := Options{}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	if opts.Height <= DefaultHeight {
		t.Errorf("Tall graph should scale height above default, got %f", opts.Height)
	}
}

func TestAdaptForGraph_PreservesUserDimensions(t *testing.T) {
	g := dag.New(nil)
	for i := range 30 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: 0})
	}

	opts := Options{Width: 500, Height: 400}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	if opts.Width != 500 {
		t.Errorf("User-set width should be preserved, got %f", opts.Width)
	}
	if opts.Height != 400 {
		t.Errorf("User-set height should be preserved, got %f", opts.Height)
	}
}

func TestAdaptForGraph_CapsDimensions(t *testing.T) {
	g := dag.New(nil)
	// Extremely wide row: 200 nodes
	for i := range 200 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: 0})
	}
	// Many layers
	for i := range 50 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("r%d", i), Row: i + 1})
	}

	opts := Options{}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	if opts.Width > 4800 {
		t.Errorf("Width should be capped at 4800, got %f", opts.Width)
	}
	if opts.Height > 3600 {
		t.Errorf("Height should be capped at 3600, got %f", opts.Height)
	}
}

func TestAdaptForGraph_PreservesAspectRatio(t *testing.T) {
	g := dag.New(nil)
	for i := range 20 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("n%d", i), Row: 0})
	}
	g.AddNode(dag.Node{ID: "root", Row: 1})

	opts := Options{}
	opts.SetLayoutDefaults()
	opts.AdaptForGraph(g)

	ratio := opts.Width / opts.Height
	expected := DefaultWidth / DefaultHeight
	if ratio < expected*0.95 || ratio > expected*1.05 {
		t.Errorf("Aspect ratio should be ~%.2f, got %.2f (%.0fx%.0f)", expected, ratio, opts.Width, opts.Height)
	}
}

func TestGenerateTowerLayout_DenseGraphUsesReducedRandomize(t *testing.T) {
	g := dag.New(nil)
	// 100 nodes across 4 rows → dense graph
	for i := range 25 {
		g.AddNode(dag.Node{ID: fmt.Sprintf("a%d", i), Row: 0})
		g.AddNode(dag.Node{ID: fmt.Sprintf("b%d", i), Row: 1})
		g.AddNode(dag.Node{ID: fmt.Sprintf("c%d", i), Row: 2})
		g.AddNode(dag.Node{ID: fmt.Sprintf("d%d", i), Row: 3})
	}
	for i := range 25 {
		g.AddEdge(dag.Edge{From: fmt.Sprintf("a%d", i), To: fmt.Sprintf("b%d", i)})
		g.AddEdge(dag.Edge{From: fmt.Sprintf("b%d", i), To: fmt.Sprintf("c%d", i)})
		g.AddEdge(dag.Edge{From: fmt.Sprintf("c%d", i), To: fmt.Sprintf("d%d", i)})
	}

	opts := Options{
		Randomize: true,
		Merge:     true,
		Ordering:  "barycentric",
	}
	opts.SetLayoutDefaults()

	layout, err := generateTowerLayout(g, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(layout.Blocks) == 0 {
		t.Error("Expected blocks in layout")
	}
}
