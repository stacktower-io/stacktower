// Package fonts provides embedded font files for SVG rendering.
//
// The fonts are embedded directly into the binary using go:embed,
// making them available without external dependencies.
package fonts

import (
	_ "embed"
	"encoding/base64"
	"sync"
)

// Gaegu is a handwriting-style font by Jiashuo Zhang & JIKJI FONT,
// licensed under the SIL Open Font License 1.1.
// https://fonts.google.com/specimen/Gaegu
// Subsetted to Latin characters only to keep binary size small.

//go:embed Gaegu-Regular.woff
var gaeguWOFF []byte

//go:embed Gaegu-Regular.ttf
var gaeguTTF []byte

// GaeguWOFF returns the WOFF font data.
func GaeguWOFF() []byte {
	return gaeguWOFF
}

// GaeguTTF returns the TTF font data.
func GaeguTTF() []byte {
	return gaeguTTF
}

var (
	woffBase64     string
	woffBase64Once sync.Once
)

// GaeguWOFFBase64 returns the WOFF font data as a base64 string.
// The result is cached after first computation.
func GaeguWOFFBase64() string {
	woffBase64Once.Do(func() {
		woffBase64 = base64.StdEncoding.EncodeToString(gaeguWOFF)
	})
	return woffBase64
}

// FontFamily is the CSS font-family name for the hand-drawn style font.
const FontFamily = "Gaegu"

// FallbackFontFamily provides fallback fonts for systems without the embedded font.
const FallbackFontFamily = `'Gaegu', 'Comic Sans MS', 'Bradley Hand', 'Segoe Script', sans-serif`
