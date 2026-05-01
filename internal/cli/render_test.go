package cli

import (
	"os"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/graph"
	"github.com/stacktower-io/stacktower/pkg/pipeline"
)

func TestReadRenderInputStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()

	graphJSON := `{"nodes":[{"id":"app"},{"id":"dep"}],"edges":[{"from":"app","to":"dep"}]}`
	if _, err := w.WriteString(graphJSON); err != nil {
		t.Fatalf("write stdin data: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	g, err := loadGraph("-")
	if err != nil {
		t.Fatalf("loadGraph(-) error = %v", err)
	}
	if g.NodeCount() != 2 {
		t.Fatalf("NodeCount = %d, want 2", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Fatalf("EdgeCount = %d, want 1", g.EdgeCount())
	}
}

func TestParseFormats(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty defaults to svg", "", []string{"svg"}},
		{"single format", "svg", []string{"svg"}},
		{"multiple formats", "svg,pdf,png", []string{"svg", "pdf", "png"}},
		{"multiple formats with spaces", "svg, pdf , png", []string{"svg", "pdf", "png"}},
		{"pdf only", "pdf", []string{"pdf"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFormats(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseFormats(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("parseFormats(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestValidateFormats(t *testing.T) {
	tests := []struct {
		name    string
		formats []string
		wantErr bool
	}{
		{"valid svg", []string{"svg"}, false},
		{"valid pdf", []string{"pdf"}, false},
		{"valid png", []string{"png"}, false},
		{"valid json", []string{"json"}, false},
		{"valid multiple", []string{"svg", "pdf", "png"}, false},
		{"valid all", []string{"svg", "pdf", "png", "json"}, false},
		{"invalid format", []string{"invalid"}, true},
		{"mixed valid invalid", []string{"svg", "invalid"}, true},
		{"empty slice", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pipeline.ValidateFormats(tt.formats)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFormats(%v) error = %v, wantErr %v", tt.formats, err, tt.wantErr)
			}
		})
	}
}

func TestValidateStyle(t *testing.T) {
	tests := []struct {
		name    string
		style   string
		wantErr bool
	}{
		{"simple", "simple", false},
		{"handdrawn", "handdrawn", false},
		{"invalid", "invalid", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pipeline.ValidateStyle(tt.style)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStyle(%q) error = %v, wantErr %v", tt.style, err, tt.wantErr)
			}
		})
	}
}

func TestIsValidFormat(t *testing.T) {
	for _, f := range []string{"svg", "pdf", "png", "json"} {
		if !pipeline.IsValidFormat(f) {
			t.Errorf("IsValidFormat(%q) = false, want true", f)
		}
	}
	if pipeline.IsValidFormat("invalid") {
		t.Error("IsValidFormat(\"invalid\") should be false")
	}
}

func TestStyleConstants(t *testing.T) {
	if graph.StyleSimple != "simple" {
		t.Errorf("graph.StyleSimple = %q, want %q", graph.StyleSimple, "simple")
	}
	if graph.StyleHanddrawn != "handdrawn" {
		t.Errorf("graph.StyleHanddrawn = %q, want %q", graph.StyleHanddrawn, "handdrawn")
	}
}

func TestDefaultConstants(t *testing.T) {
	if pipeline.DefaultWidth != 800 {
		t.Errorf("pipeline.DefaultWidth = %v, want 800", pipeline.DefaultWidth)
	}
	if pipeline.DefaultHeight != 600 {
		t.Errorf("pipeline.DefaultHeight = %v, want 600", pipeline.DefaultHeight)
	}
	if pipeline.DefaultSeed != 42 {
		t.Errorf("pipeline.DefaultSeed = %v, want 42", pipeline.DefaultSeed)
	}
}
