package handdrawn

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/core/render/tower/styles"
)

func TestNew(t *testing.T) {
	h := New(42)
	if h == nil {
		t.Fatal("New() returned nil")
		return
	}
	if h.seed != 42 {
		t.Errorf("seed = %d, want 42", h.seed)
	}
}

func TestHandDrawn_RenderDefs(t *testing.T) {
	h := New(42)
	var buf bytes.Buffer
	h.RenderDefs(&buf)

	output := buf.String()
	if !strings.Contains(output, "<defs>") {
		t.Error("RenderDefs() missing <defs> tag")
	}
	if !strings.Contains(output, "Gaegu") {
		t.Error("RenderDefs() missing font-face declaration")
	}
	if !strings.Contains(output, "data:font/woff;base64,") {
		t.Error("RenderDefs() missing embedded font data")
	}
	if !strings.Contains(output, "brittleTexture") {
		t.Error("RenderDefs() missing brittleTexture pattern")
	}
}

func TestHandDrawn_RenderBlock(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID:    "test-block",
		Label: "Test Label",
		X:     10, Y: 20, W: 100, H: 50,
		CX: 60, CY: 45,
		URL: "http://example.com",
	}

	var buf bytes.Buffer
	h.RenderBlock(&buf, block)
	output := buf.String()

	if !strings.Contains(output, `id="block-test-block"`) {
		t.Errorf("RenderBlock() missing block id: %s", output)
	}
	if !strings.Contains(output, `class="block"`) {
		t.Errorf("RenderBlock() missing block class: %s", output)
	}
	if !strings.Contains(output, `href="http://example.com"`) {
		t.Errorf("RenderBlock() missing URL: %s", output)
	}
}

func TestHandDrawn_RenderBlock_Brittle(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID:    "brittle-block",
		Label: "Brittle",
		X:     10, Y: 20, W: 100, H: 50,
		CX: 60, CY: 45,
		Brittle: true,
	}

	var buf bytes.Buffer
	h.RenderBlock(&buf, block)
	output := buf.String()

	if !strings.Contains(output, `class="block brittle"`) {
		t.Errorf("RenderBlock() brittle block missing brittle class: %s", output)
	}
	if !strings.Contains(output, `class="block-texture"`) {
		t.Errorf("RenderBlock() brittle block missing texture: %s", output)
	}
	if !strings.Contains(output, `fill="url(#brittleTexture)"`) {
		t.Errorf("RenderBlock() brittle block missing texture fill: %s", output)
	}
}

func TestHandDrawn_RenderEdge(t *testing.T) {
	h := New(42)
	edge := styles.Edge{
		FromID: "from",
		ToID:   "to",
		X1:     10, Y1: 10,
		X2: 100, Y2: 100,
	}

	var buf bytes.Buffer
	h.RenderEdge(&buf, edge)
	output := buf.String()

	if !strings.Contains(output, `class="edge"`) {
		t.Errorf("RenderEdge() missing edge class: %s", output)
	}
	if !strings.Contains(output, `<path`) {
		t.Errorf("RenderEdge() missing path element: %s", output)
	}
	if !strings.Contains(output, `stroke-dasharray`) {
		t.Errorf("RenderEdge() missing dash array: %s", output)
	}
}

func TestHandDrawn_RenderText(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID:    "text-block",
		Label: "Text Label",
		X:     10, Y: 20, W: 100, H: 50,
		CX: 60, CY: 45,
		URL: "http://example.com",
	}

	var buf bytes.Buffer
	h.RenderText(&buf, block)
	output := buf.String()

	if !strings.Contains(output, `class="block-text"`) {
		t.Errorf("RenderText() missing block-text class: %s", output)
	}
	if !strings.Contains(output, `data-block="text-block"`) {
		t.Errorf("RenderText() missing data-block attr: %s", output)
	}
	if !strings.Contains(output, `<text`) {
		t.Errorf("RenderText() missing text element: %s", output)
	}
	if !strings.Contains(output, "Gaegu") {
		t.Errorf("RenderText() missing font family: %s", output)
	}
}

func TestHandDrawn_RenderText_Rotated(t *testing.T) {
	h := New(42)
	// Tall block that should trigger rotation
	block := styles.Block{
		ID:    "tall-block",
		Label: "Tall Block",
		X:     10, Y: 20, W: 20, H: 150,
		CX: 20, CY: 95,
	}

	var buf bytes.Buffer
	h.RenderText(&buf, block)
	output := buf.String()

	if !strings.Contains(output, `transform="rotate(-90`) {
		t.Errorf("RenderText() rotated text missing rotate transform: %s", output)
	}
}

func TestHandDrawn_RenderPopup_Nil(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID:    "no-popup",
		Popup: nil,
	}

	var buf bytes.Buffer
	h.RenderPopup(&buf, block)

	if buf.Len() != 0 {
		t.Errorf("RenderPopup() should produce no output for nil popup, got: %s", buf.String())
	}
}

func TestHandDrawn_RenderPopup(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID:    "popup-block",
		Label: "Popup Block",
		Popup: &styles.PopupData{
			Description: "A description of the package",
			Stars:       1500,
			LastCommit:  "2024-01-15",
			LastRelease: "2024-01-10",
		},
	}

	var buf bytes.Buffer
	h.RenderPopup(&buf, block)
	output := buf.String()

	if !strings.Contains(output, `class="popup"`) {
		t.Errorf("RenderPopup() missing popup class: %s", output)
	}
	if !strings.Contains(output, `data-for="popup-block"`) {
		t.Errorf("RenderPopup() missing data-for attr: %s", output)
	}
	if !strings.Contains(output, "A description of the package") {
		t.Errorf("RenderPopup() missing description: %s", output)
	}
	if !strings.Contains(output, "★") {
		t.Errorf("RenderPopup() missing stars: %s", output)
	}
	if !strings.Contains(output, "1.5k") {
		t.Errorf("RenderPopup() missing formatted stars: %s", output)
	}
	if !strings.Contains(output, "last commit: 2024-01-15") {
		t.Errorf("RenderPopup() missing last commit: %s", output)
	}
}

func TestHandDrawn_RenderPopup_Brittle(t *testing.T) {
	h := New(42)
	block := styles.Block{
		ID: "brittle-popup",
		Popup: &styles.PopupData{
			Description: "A brittle package",
			Archived:    true,
			Brittle:     true,
			LastCommit:  "2020-01-01",
		},
	}

	var buf bytes.Buffer
	h.RenderPopup(&buf, block)
	output := buf.String()

	if !strings.Contains(output, "⚠") {
		t.Errorf("RenderPopup() brittle should have warning symbol: %s", output)
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{500, "500"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{999999, "1000.0k"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{10000000, "10.0M"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatNumber(tt.n); got != tt.want {
				t.Errorf("formatNumber(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxChars int
		wantLen  int
	}{
		{"short text", "hello", 10, 1},
		{"exact length", "hello world", 11, 1},
		{"needs wrap", "hello world foo", 6, 3},
		{"with newlines", "hello\nworld", 20, 1},
		{"empty", "", 10, 1},
		{"long single word", "supercalifragilisticexpialidocious", 10, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := wrapText(tt.text, tt.maxChars)
			if len(lines) != tt.wantLen {
				t.Errorf("wrapText(%q, %d) returned %d lines, want %d: %v",
					tt.text, tt.maxChars, len(lines), tt.wantLen, lines)
			}
		})
	}
}
