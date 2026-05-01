package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

// quietMode suppresses non-essential output when set to true.
// Set via SetQuiet() at startup.
var quietMode bool

// SetQuiet sets the quiet mode flag.
func SetQuiet(q bool) {
	quietMode = q
}

// IsQuiet returns the current quiet mode state.
func IsQuiet() bool {
	return quietMode
}

// =============================================================================
// Shared Spinner Frames
// =============================================================================

// SpinnerFrames is the shared animation sequence for all spinners and progress views.
// Using a consistent animation provides a cohesive visual experience across all CLI operations.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerInterval is the shared animation speed for all spinners.
const SpinnerInterval = 80 * time.Millisecond

// =============================================================================
// Operation Interface
// =============================================================================

// Operation represents a running CLI operation with progress indication.
// Both Spinner and ProgressView implement this interface, allowing code
// to work with either depending on the complexity of the operation.
//
// Use Spinner for simple operations (network fetches, file I/O).
// Use ProgressView for complex resolver operations with observability hooks.
type Operation interface {
	// Stop halts the operation's progress display.
	Stop()
	// StopWithError stops and displays an error message.
	StopWithError(message string)
}

// =============================================================================
// Status Output
// =============================================================================

// PrintSuccess prints a success message to stderr. Suppressed in quiet mode.
func PrintSuccess(format string, args ...any) {
	if quietMode {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, StyleIconSuccess.Render(IconSuccess)+" "+msg)
}

// PrintError prints an error message to stderr. Never suppressed.
func PrintError(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, StyleIconError.Render(IconError)+" "+StyleError.Render(msg))
}

// PrintWarning prints a warning message to stderr. Never suppressed.
func PrintWarning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, StyleIconWarning.Render(IconWarning)+" "+StyleWarning.Render(msg))
}

// PrintInfo prints an info/status message to stderr. Suppressed in quiet mode.
func PrintInfo(format string, args ...any) {
	if quietMode {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, StyleIconInfo.Render(IconInfo)+" "+msg)
}

// PrintDetail prints a detail line (indented) to stderr. Suppressed in quiet mode.
func PrintDetail(format string, args ...any) {
	if quietMode {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "  "+StyleDim.Render(msg))
}

// =============================================================================
// File Output
// =============================================================================

// PrintFile prints a file output line to stderr. Suppressed in quiet mode.
func PrintFile(path string) {
	if quietMode {
		return
	}
	fmt.Fprintln(os.Stderr, "  "+StyleDim.Render(IconArrow)+" "+StyleValue.Render(path))
}

// =============================================================================
// Key-Value Output
// =============================================================================

// PrintKeyValue prints a labeled value to stderr.
func PrintKeyValue(key, value string) {
	fmt.Fprintln(os.Stderr, StyleKeyLabel.Render(key)+" "+StyleValue.Render(value))
}

// =============================================================================
// Stats Display
// =============================================================================

// PrintStats prints graph statistics on a single line to stderr. Suppressed in quiet mode.
// Pass depth <= 0 to omit the depth field. Pass elapsed <= 0 to omit timing.
func PrintStats(nodeCount, edgeCount, depth int, cached bool, elapsed time.Duration) {
	if quietMode {
		return
	}
	parts := []string{
		StyleNumber.Render(fmt.Sprintf("%d", nodeCount)) + StyleDim.Render(" packages"),
		StyleNumber.Render(fmt.Sprintf("%d", edgeCount)) + StyleDim.Render(" edges"),
	}
	if depth > 0 {
		parts = append(parts, StyleDim.Render("depth ")+StyleNumber.Render(fmt.Sprintf("%d", depth)))
	}

	status := IconFresh
	statusStyle := StyleComputed
	if cached {
		status = IconCached
		statusStyle = StyleCached
	}
	parts = append(parts, statusStyle.Render(status))

	if elapsed > 0 {
		parts = append(parts, StyleDim.Render(FormatDuration(elapsed)))
	}

	fmt.Fprintln(os.Stderr, "  "+JoinDot(parts))
}

// FormatDuration formats a duration for human-readable display.
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// RenderStats holds curated render output info.
type RenderStats struct {
	Layers      int
	Crossings   int
	OrderingRan bool // true when layout ordering was computed (not just visualization)
	Ordering    string
	Style       string
	Dimensions  string
}

// PrintRenderStats prints a curated summary of the render operation.
func PrintRenderStats(s RenderStats) {
	if quietMode {
		return
	}
	parts := []string{
		StyleNumber.Render(fmt.Sprintf("%d", s.Layers)) + StyleDim.Render(" layers"),
	}

	if s.OrderingRan {
		if s.Crossings == 0 {
			parts = append(parts, StyleSuccess.Render("0")+StyleDim.Render(" crossings"))
		} else {
			parts = append(parts, StyleWarning.Render(fmt.Sprintf("%d", s.Crossings))+StyleDim.Render(" crossings"))
		}
	}

	parts = append(parts, StyleDim.Render(s.Ordering))
	parts = append(parts, StyleDim.Render(s.Style))

	fmt.Fprintln(os.Stderr, "  "+JoinDot(parts))
}

// JoinDot joins parts with a dim " · " separator.
func JoinDot(parts []string) string {
	sep := StyleDim.Render(" · ")
	return strings.Join(parts, sep)
}

// =============================================================================
// Section Headers
//
// The CLI has two canonical section styles, intentionally visually distinct:
//
//   PrintHeader    — a bold title with a full-width underline on the next
//                    line. Use for TOP-LEVEL section banners that open a
//                    command's output (e.g. "Cache", "whoami").
//
//   PrintSeparator — a short "─── title ───" divider with vertical padding.
//                    Use to delimit INLINE sub-sections within a single
//                    command's output (e.g. "Dependency Tree" inside
//                    `parse`).
//
// Keep new callers in one of these two modes — ad-hoc `Bold(title)` prints
// drift over time and make the output look inconsistent.
// =============================================================================

// PrintHeader prints a styled top-level section header to stderr. Used for
// multi-phase commands (e.g. GitHub flow) and informational displays (e.g.
// whoami). Suppressed in quiet mode.
func PrintHeader(title string) {
	if quietMode {
		return
	}
	fmt.Fprintln(os.Stderr, StyleTitle.Render(title))
	fmt.Fprintln(os.Stderr, StyleDim.Render(strings.Repeat("─", runewidth.StringWidth(title)+2)))
}

// =============================================================================
// Commands & Next Steps
// =============================================================================

// PrintNextStep prints a suggested next command to stderr. Suppressed in quiet mode.
func PrintNextStep(description, cmd string) {
	if quietMode {
		return
	}
	fmt.Fprintln(os.Stderr, StyleDim.Render(description+":"+" ")+StyleCommand.Render(cmd))
}

// =============================================================================
// Version Display
// =============================================================================

// PrintVersionInfo prints styled version information to stderr.
func PrintVersionInfo(version, commit, date string) {
	fmt.Fprintln(os.Stderr, StyleTitle.Render("stacktower")+" "+StyleHighlight.Render(version))
	PrintKeyValue("Commit", commit)
	PrintKeyValue("Built", date)
}

// =============================================================================
// Utilities
// =============================================================================

// PrintInline prints a dim message to stderr without a trailing newline.
func PrintInline(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprint(os.Stderr, StyleDim.Render(msg))
}

// PrintNewline prints an empty line to stderr. Suppressed in quiet mode.
func PrintNewline() {
	if quietMode {
		return
	}
	fmt.Fprintln(os.Stderr)
}

// PrintSeparator prints a styled inline separator ("─── Title ───") on its
// own line, padded with blank lines above and below. Use this to delimit
// sub-sections WITHIN a single command's output (not as a top-level banner —
// use PrintHeader for that). Suppressed in quiet mode.
func PrintSeparator(title string) {
	if quietMode {
		return
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, StyleDim.Render("─── "+title+" ───"))
	fmt.Fprintln(os.Stderr)
}

// FormatRuntimeSource returns a human-readable parenthesized label for the
// given runtime-source tag as emitted by the pipeline (e.g. "cli",
// "manifest", "package", "default"). Unknown values return the empty string.
func FormatRuntimeSource(source string) string {
	switch source {
	case "cli":
		return "(from --runtime-version)"
	case "manifest":
		return "(from manifest)"
	case "package":
		return "(from package)"
	case "default":
		return "(default)"
	default:
		return ""
	}
}

// PrintRuntimeInfo prints a styled "Runtime: <lang> <version> (source)" line
// to stderr followed by a blank line. Suppressed in quiet mode.
// If version is empty, this is a no-op.
func PrintRuntimeInfo(langName, version, source string) {
	if quietMode || version == "" {
		return
	}
	runtimeInfo := fmt.Sprintf("Runtime: %s %s", langName, version)
	if label := FormatRuntimeSource(source); label != "" {
		runtimeInfo += " " + StyleDim.Render(label)
	}
	fmt.Fprintln(os.Stderr, StyleInfo.Render(runtimeInfo))
	fmt.Fprintln(os.Stderr)
}

// SupportedManifestList returns a comma-separated, alphabetically sorted
// list of supported manifest filenames across the given languages.
//
// The result for the global languages.All slice is cached because it's hit
// from error paths that can fire multiple times per command (e.g. `resolve`
// surfacing both an argument hint and a file-not-found hint in one run),
// and the list is stable for the lifetime of the process.
func SupportedManifestList(langs []*deps.Language) string {
	supportedManifestsOnce.Do(func() {
		supportedManifestsCache = buildSupportedManifestList(langs)
		supportedManifestsCacheKey = langCacheKey(langs)
	})
	// If the caller passes a different slice we honour it without caching,
	// which keeps the helper correct for edge cases like tests.
	if langCacheKey(langs) != supportedManifestsCacheKey {
		return buildSupportedManifestList(langs)
	}
	return supportedManifestsCache
}

var (
	supportedManifestsOnce     sync.Once
	supportedManifestsCache    string
	supportedManifestsCacheKey string
)

// langCacheKey builds a stable identity string from the language names so the
// cache can detect when a different language set is passed.
func langCacheKey(langs []*deps.Language) string {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = l.Name
	}
	return strings.Join(names, ",")
}

func buildSupportedManifestList(langs []*deps.Language) string {
	manifests := deps.SupportedManifests(langs)
	files := make([]string, 0, len(manifests))
	for f := range manifests {
		files = append(files, f)
	}
	slices.Sort(files)
	return strings.Join(files, ", ")
}
