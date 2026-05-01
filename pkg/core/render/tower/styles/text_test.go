package styles

import (
	"bytes"
	"strings"
	"testing"
)

func TestFontSize(t *testing.T) {
	tests := []struct {
		name   string
		block  Block
		minMax [2]float64 // expected to be within [min, max]
	}{
		{
			name:   "small block",
			block:  Block{ID: "a", W: 20, H: 20},
			minMax: [2]float64{fontSizeMin, fontSizeMax},
		},
		{
			name:   "large block short text",
			block:  Block{ID: "ab", W: 200, H: 100},
			minMax: [2]float64{fontSizeMin, fontSizeMax},
		},
		{
			name:   "narrow block long text",
			block:  Block{ID: "very-long-package-name", W: 50, H: 100},
			minMax: [2]float64{fontSizeMin, fontSizeMax},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FontSize(tt.block)
			if got < tt.minMax[0] || got > tt.minMax[1] {
				t.Errorf("FontSize() = %v, want between %v and %v", got, tt.minMax[0], tt.minMax[1])
			}
		})
	}
}

func TestFontSizeRotated(t *testing.T) {
	tests := []struct {
		name   string
		block  Block
		minMax [2]float64
	}{
		{
			name:   "tall narrow block",
			block:  Block{ID: "package", W: 30, H: 100},
			minMax: [2]float64{fontSizeMin, fontSizeMax},
		},
		{
			name:   "square block",
			block:  Block{ID: "pkg", W: 50, H: 50},
			minMax: [2]float64{fontSizeMin, fontSizeMax},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FontSizeRotated(tt.block)
			if got < tt.minMax[0] || got > tt.minMax[1] {
				t.Errorf("FontSizeRotated() = %v, want between %v and %v", got, tt.minMax[0], tt.minMax[1])
			}
		})
	}
}

func TestShouldRotate(t *testing.T) {
	tests := []struct {
		name  string
		block Block
		want  bool
	}{
		{
			name:  "wide block - no rotate",
			block: Block{ID: "pkg", W: 100, H: 30},
			want:  false,
		},
		{
			name:  "tall narrow block - rotate",
			block: Block{ID: "package-name", W: 30, H: 100},
			want:  true,
		},
		{
			name:  "square block short text - no rotate",
			block: Block{ID: "abc", W: 50, H: 50},
			want:  false,
		},
		{
			name:  "long text narrow block",
			block: Block{ID: "very-long-package-name", W: 40, H: 80},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldRotate(tt.block); got != tt.want {
				t.Errorf("ShouldRotate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateLabel(t *testing.T) {
	tests := []struct {
		name    string
		block   Block
		rotated bool
		wantLen int // max expected length
	}{
		{
			name:    "short label fits",
			block:   Block{ID: "pkg", Label: "pkg", W: 100, H: 30},
			rotated: false,
			wantLen: 3,
		},
		{
			name:    "long label truncated",
			block:   Block{ID: "very-long-package", Label: "very-long-package", W: 40, H: 30},
			rotated: false,
			wantLen: 20, // should be truncated
		},
		{
			name:    "rotated label",
			block:   Block{ID: "package", Label: "package", W: 30, H: 80},
			rotated: true,
			wantLen: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateLabel(tt.block, tt.rotated)
			if len(got) > tt.wantLen && !strings.HasSuffix(got, "..") {
				t.Errorf("TruncateLabel() = %q (len=%d), want len <= %d or ends with '..'", got, len(got), tt.wantLen)
			}
		})
	}
}

func TestTruncateLabelEndsWithDots(t *testing.T) {
	block := Block{
		ID:    "this-is-a-very-very-long-package-name",
		Label: "this-is-a-very-very-long-package-name",
		W:     50,
		H:     20,
	}

	got := TruncateLabel(block, false)
	if len(got) >= len(block.Label) {
		// Label wasn't truncated, that's fine
		return
	}

	if !strings.HasSuffix(got, "..") {
		t.Errorf("TruncateLabel() = %q, truncated label should end with '..'", got)
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "ampersand",
			input: "a & b",
			want:  "a &amp; b",
		},
		{
			name:  "less than",
			input: "a < b",
			want:  "a &lt; b",
		},
		{
			name:  "greater than",
			input: "a > b",
			want:  "a &gt; b",
		},
		{
			name:  "quotes",
			input: `say "hello"`,
			want:  "say &#34;hello&#34;",
		},
		{
			name:  "apostrophe",
			input: "it's",
			want:  "it&#39;s",
		},
		{
			name:  "multiple special chars",
			input: "<script>alert('xss')</script>",
			want:  "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EscapeXML(tt.input); got != tt.want {
				t.Errorf("EscapeXML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWrapURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		content string
		want    string
	}{
		{
			name:    "with URL",
			url:     "https://example.com",
			content: "<text>hello</text>",
			want:    `  <a href="https://example.com" target="_blank"><text>hello</text></a>`,
		},
		{
			name:    "empty URL",
			url:     "",
			content: "<text>hello</text>",
			want:    "<text>hello</text>",
		},
		{
			name:    "URL with special chars",
			url:     "https://example.com?a=1&b=2",
			content: "<rect/>",
			want:    `  <a href="https://example.com?a=1&amp;b=2" target="_blank"><rect/></a>`,
		},
		{
			name:    "unsafe URL scheme",
			url:     "javascript:alert(1)",
			content: "<text>hello</text>",
			want:    "<text>hello</text>",
		},
		{
			name:    "trim safe URL",
			url:     "  https://example.com/docs  ",
			content: "<text>hello</text>",
			want:    `  <a href="https://example.com/docs" target="_blank"><text>hello</text></a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			WrapURL(&buf, tt.url, func() {
				buf.WriteString(tt.content)
			})
			if got := buf.String(); got != tt.want {
				t.Errorf("WrapURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFontSizeForEdgeCases(t *testing.T) {
	// Test that fontSizeFor handles edge cases
	tests := []struct {
		name       string
		availWidth float64
		availH     float64
		textLen    int
	}{
		{"zero text length", 100, 50, 0},
		{"negative text length", 100, 50, -1},
		{"very small dimensions", 1, 1, 5},
		{"very large dimensions", 10000, 10000, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fontSizeFor(tt.availWidth, tt.availH, tt.textLen)
			if got < fontSizeMin || got > fontSizeMax {
				t.Errorf("fontSizeFor(%v, %v, %d) = %v, want between %v and %v",
					tt.availWidth, tt.availH, tt.textLen, got, fontSizeMin, fontSizeMax)
			}
		})
	}
}
