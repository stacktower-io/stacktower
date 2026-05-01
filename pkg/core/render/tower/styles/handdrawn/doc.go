// Package handdrawn provides an XKCD-inspired hand-drawn visual style.
//
// # Overview
//
// This style creates the signature wobbly, sketchy aesthetic inspired by
// Randall Munroe's XKCD comics. It's the default style for Stacktower,
// matching the hand-drawn appearance of the original XKCD #2347.
//
// # Visual Elements
//
// The hand-drawn style includes:
//
//   - Wobbly lines: SVG displacement filters create imperfect edges
//   - Rough fills: Textured backgrounds instead of solid colors
//   - Comic fonts: Gaegu typeface (OFL-licensed)
//   - Sketchy borders: Multiple offset strokes for a drawn look
//   - Brittle treatment: Red tinting for at-risk packages
//
// # Reproducible Randomness
//
// The style uses a seed value to ensure consistent "randomness":
//
//	style := handdrawn.New(42)  // Same seed = same wobble pattern
//
// This means the same graph rendered twice with the same seed will look
// identical, which is important for caching and reproducibility.
//
// # Colors
//
// The color scheme mimics hand-colored technical drawings:
//
//   - Muted pastels for block fills
//   - Dark outlines (not pure black)
//   - Red/orange tints for brittle packages
//   - Paper-like background texture
//
// See [colors.go] for the full palette.
//
// # Usage
//
//	style := handdrawn.New(seed)
//	svg := sink.RenderSVG(layout,
//	    sink.WithStyle(style),
//	    sink.WithPopups(),
//	)
//
// # Implementation
//
// The hand-drawn effect is achieved through:
//
//   - SVG <feTurbulence> and <feDisplacementMap> filters
//   - Multiple slightly-offset path renders
//   - Bezier curves with jittered control points
//   - Base64-encoded texture images for fills
package handdrawn
