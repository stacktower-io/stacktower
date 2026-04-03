package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"github.com/matzehuels/stacktower/pkg/observability"
)

// maxDisplayedNames is the maximum number of package names shown in the "fetching" line.
const maxDisplayedNames = 5

// ProgressView renders live resolver progress on stderr.
// It implements both observability.ResolverHooks and observability.RateLimitHooks
// so the crawler and HTTP clients feed it events without any direct coupling to CLI code.
//
// ProgressView uses shared spinner frames from styles.go for consistent visual
// appearance with the simpler Spinner component.
type ProgressView struct {
	message  string
	maxNodes int

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	isTTY  bool
	active bool

	mu       sync.Mutex
	inflight map[string]bool
	fetched  int // unique package names fetched successfully (deduped)
	seenOK   map[string]bool
	pending  int // remaining jobs (updated in collector via OnProgress)
	lines    int // how many lines were last printed (for clearing)

	rateLimited   map[string]rateLimitInfo // registry -> rate limit info
	circuitStates map[string]circuitInfo   // registry -> circuit breaker info
	throttled     map[string]throttleInfo  // registry -> throttle info with end time

	enriching      bool   // true when batch enrichment is in progress
	enrichProvider string // name of the provider doing enrichment (e.g., "github")
	enrichCount    int    // number of packages being enriched
}

type throttleInfo struct {
	startedAt time.Time
	endsAt    time.Time
}

type rateLimitInfo struct {
	hitAt      time.Time
	retryAfter int
}

type circuitInfo struct {
	state observability.CircuitState
	until time.Time
}

// NewProgressView creates a new progress view.
func NewProgressView(ctx context.Context, message string, maxNodes int) *ProgressView {
	pvCtx, cancel := context.WithCancel(ctx)
	return &ProgressView{
		message:       message,
		maxNodes:      maxNodes,
		ctx:           pvCtx,
		cancel:        cancel,
		done:          make(chan struct{}),
		isTTY:         isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()),
		inflight:      make(map[string]bool),
		seenOK:        make(map[string]bool),
		rateLimited:   make(map[string]rateLimitInfo),
		circuitStates: make(map[string]circuitInfo),
		throttled:     make(map[string]throttleInfo),
	}
}

// Start registers hooks and launches the render loop.
func (pv *ProgressView) Start() {
	if quietMode {
		return
	}
	observability.SetResolverHooks(pv)
	observability.SetRateLimitHooks(pv)
	pv.active = true
	if !pv.isTTY {
		return
	}
	pv.render(0)
	go pv.loop()
}

// Stop deregisters hooks, cancels the render loop, and clears the output area.
func (pv *ProgressView) Stop() {
	if !pv.active {
		return
	}
	pv.cancel()
	if pv.isTTY {
		<-pv.done
		pv.mu.Lock()
		pv.clearLinesLocked()
		pv.mu.Unlock()
	}
	observability.SetResolverHooks(nil)
	observability.SetRateLimitHooks(nil)
	pv.active = false
}

// StopWithError deregisters hooks, clears progress, and prints an error.
func (pv *ProgressView) StopWithError(message string) {
	if !pv.active {
		return
	}
	pv.cancel()
	if pv.isTTY {
		<-pv.done
		pv.mu.Lock()
		pv.clearLinesLocked()
		pv.mu.Unlock()
	}
	observability.SetResolverHooks(nil)
	observability.SetRateLimitHooks(nil)
	pv.active = false
	PrintError("%s", message)
}

// ---------------------------------------------------------------------------
// observability.ResolverHooks implementation
// ---------------------------------------------------------------------------

func (pv *ProgressView) OnFetchStart(_ context.Context, pkg string, _ int) {
	pv.mu.Lock()
	pv.inflight[pkg] = true
	pv.mu.Unlock()
}

func (pv *ProgressView) OnFetchComplete(_ context.Context, pkg string, _ int, _ int, err error) {
	pv.mu.Lock()
	delete(pv.inflight, pkg)
	if err == nil {
		if !pv.seenOK[pkg] {
			pv.seenOK[pkg] = true
			pv.fetched++
		}
	}
	pv.mu.Unlock()
}

func (pv *ProgressView) OnProgress(_ context.Context, _, pending, _ int) {
	pv.mu.Lock()
	pv.pending = pending
	pv.mu.Unlock()
}

func (pv *ProgressView) OnEnrichStart(_ context.Context, provider string, count int) {
	pv.mu.Lock()
	pv.enriching = true
	pv.enrichProvider = provider
	pv.enrichCount = count
	pv.mu.Unlock()
}

func (pv *ProgressView) OnEnrichComplete(_ context.Context, _ string, _ int, _ error) {
	pv.mu.Lock()
	pv.enriching = false
	pv.enrichProvider = ""
	pv.enrichCount = 0
	pv.mu.Unlock()
}

// ---------------------------------------------------------------------------
// observability.RateLimitHooks implementation
// ---------------------------------------------------------------------------

func (pv *ProgressView) OnRateLimitWait(_ context.Context, registry string, wait time.Duration) {
	if wait < 10*time.Millisecond {
		return
	}
	now := time.Now()
	pv.mu.Lock()
	pv.throttled[registry] = throttleInfo{
		startedAt: now,
		endsAt:    now.Add(wait),
	}
	pv.mu.Unlock()
}

func (pv *ProgressView) OnRetry(_ context.Context, registry string, attempt int, backoff time.Duration) {
	pv.mu.Lock()
	pv.rateLimited[registry] = rateLimitInfo{
		hitAt:      time.Now(),
		retryAfter: int(backoff.Seconds()),
	}
	pv.mu.Unlock()
}

func (pv *ProgressView) OnRateLimitHit(_ context.Context, registry string, retryAfterSeconds int) {
	pv.mu.Lock()
	pv.rateLimited[registry] = rateLimitInfo{
		hitAt:      time.Now(),
		retryAfter: retryAfterSeconds,
	}
	pv.mu.Unlock()
}

func (pv *ProgressView) OnCircuitStateChange(_ context.Context, registry string, state observability.CircuitState, until time.Time) {
	pv.mu.Lock()
	if state == observability.CircuitClosed {
		delete(pv.circuitStates, registry)
	} else {
		pv.circuitStates[registry] = circuitInfo{
			state: state,
			until: until,
		}
	}
	pv.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func (pv *ProgressView) loop() {
	defer close(pv.done)
	ticker := time.NewTicker(SpinnerInterval)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-pv.ctx.Done():
			return
		case <-ticker.C:
			pv.render(i)
			i++
		}
	}
}

func (pv *ProgressView) render(frame int) {
	pv.mu.Lock()
	defer pv.mu.Unlock()

	fetched := pv.fetched
	inflight := len(pv.inflight)
	names := pv.sortedInflightLocked()
	enriching := pv.enriching
	enrichProvider := pv.enrichProvider
	enrichCount := pv.enrichCount

	spinner := SpinnerFrames[frame%len(SpinnerFrames)]

	var lines []string

	// When no fetch activity has occurred yet (lockfile parsers, cache hits),
	// show the human-readable message instead of the uninformative "0 fetched" counter.
	if fetched == 0 && inflight == 0 && !enriching && pv.message != "" {
		lines = append(lines, fmt.Sprintf("%s %s", StyleIconSpinner.Render(spinner), pv.message))
	} else {
		statsLine := fmt.Sprintf("%s %s unique fetched · %s in-flight",
			StyleIconSpinner.Render(spinner),
			StyleNumber.Render(fmt.Sprintf("%d", fetched)),
			StyleNumber.Render(fmt.Sprintf("%d", inflight)))
		lines = append(lines, statsLine)

		if len(names) > 0 {
			display := names
			suffix := ""
			if len(display) > maxDisplayedNames {
				suffix = fmt.Sprintf(" +%d more", len(display)-maxDisplayedNames)
				display = display[:maxDisplayedNames]
			}
			fetchLine := fmt.Sprintf("  fetching: %s%s",
				strings.Join(display, ", "),
				suffix)
			lines = append(lines, fetchLine)
		}

		// Show enrichment status when batch enrichment is in progress
		if enriching && enrichCount > 0 {
			enrichLine := fmt.Sprintf("  enriching %d packages with %s...",
				enrichCount, enrichProvider)
			lines = append(lines, StyleDim.Render(enrichLine))
		}
	}

	rateLimitLines := pv.rateLimitStatusLinesLocked()
	lines = append(lines, rateLimitLines...)

	width := terminalWidth()
	if len(lines) > 0 {
		lines[0] = FitToWidth(lines[0], width)
	}
	if len(lines) > 1 {
		lines[1] = StyleDim.Render(FitToWidth(lines[1], width))
	}
	for i := 2; i < len(lines); i++ {
		lines[i] = FitToWidth(lines[i], width)
	}

	pv.clearLinesLocked()
	for _, l := range lines {
		fmt.Fprintf(os.Stderr, "%s\033[K\n", l)
	}
	pv.lines = len(lines)
}

func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}

// FitToWidth truncates a string to fit within a terminal width.
func FitToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := runewidth.StringWidth(s)
	if w <= width {
		return s
	}
	if width <= 1 {
		return ""
	}

	max := width - 1 // reserve one column for ellipsis
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		if cur+rw > max {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return strings.TrimRight(b.String(), " ") + "…"
}

func (pv *ProgressView) clearLinesLocked() {
	if pv.lines == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\033[%dA", pv.lines)
	for range pv.lines {
		fmt.Fprintf(os.Stderr, "\033[K\n")
	}
	fmt.Fprintf(os.Stderr, "\033[%dA", pv.lines)
	pv.lines = 0
}

func (pv *ProgressView) sortedInflightLocked() []string {
	names := make([]string, 0, len(pv.inflight))
	for name := range pv.inflight {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (pv *ProgressView) rateLimitStatusLinesLocked() []string {
	var lines []string
	now := time.Now()

	for registry, info := range pv.circuitStates {
		var msg string
		if info.state == observability.CircuitOpen {
			remaining := info.until.Sub(now)
			if remaining > 0 {
				msg = fmt.Sprintf("  %s %s: circuit open, pausing for %s",
					StyleIconWarning.Render(IconWarning),
					registry,
					formatDuration(remaining))
			} else {
				msg = fmt.Sprintf("  %s %s: circuit open, testing recovery",
					StyleIconWarning.Render(IconWarning),
					registry)
			}
		} else if info.state == observability.CircuitHalfOpen {
			msg = fmt.Sprintf("  %s %s: testing if rate limit cleared",
				StyleIconWarning.Render(IconWarning),
				registry)
		}
		if msg != "" {
			lines = append(lines, StyleWarning.Render(msg))
		}
	}

	for registry, info := range pv.rateLimited {
		if _, hasCircuit := pv.circuitStates[registry]; hasCircuit {
			continue
		}

		elapsed := now.Sub(info.hitAt)
		if info.retryAfter > 0 {
			remaining := time.Duration(info.retryAfter)*time.Second - elapsed
			if remaining > 0 {
				msg := fmt.Sprintf("  %s %s: rate limited, waiting %s",
					StyleIconWarning.Render(IconWarning),
					registry,
					formatDuration(remaining))
				lines = append(lines, StyleWarning.Render(msg))
			}
		} else if elapsed < 5*time.Second {
			msg := fmt.Sprintf("  %s %s: rate limited",
				StyleIconWarning.Render(IconWarning),
				registry)
			lines = append(lines, StyleWarning.Render(msg))
		}
	}

	for registry, info := range pv.throttled {
		if _, hasCircuit := pv.circuitStates[registry]; hasCircuit {
			continue
		}
		if _, hasRateLimit := pv.rateLimited[registry]; hasRateLimit {
			continue
		}

		remaining := info.endsAt.Sub(now)
		if remaining > 0 {
			msg := fmt.Sprintf("  %s %s: throttling requests (%s remaining)",
				StyleIconInfo.Render(IconInfo),
				registry,
				formatDuration(remaining))
			lines = append(lines, StyleDim.Render(msg))
		} else if now.Sub(info.startedAt) < 2*time.Second {
			msg := fmt.Sprintf("  %s %s: throttling requests",
				StyleIconInfo.Render(IconInfo),
				registry)
			lines = append(lines, StyleDim.Render(msg))
		}
	}

	return lines
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm%ds", mins, secs)
}

// ProgressWriter wraps an io.Writer so that any write (e.g. from a logger)
// first clears the progress display. The next render tick redraws it below
// the newly written content, preventing interleaved output.
type ProgressWriter struct {
	pv *ProgressView
	w  io.Writer
}

// NewProgressWriter creates a writer that clears progress before writing.
func NewProgressWriter(pv *ProgressView, w io.Writer) *ProgressWriter {
	return &ProgressWriter{pv: pv, w: w}
}

func (pw *ProgressWriter) Write(p []byte) (n int, err error) {
	pw.pv.mu.Lock()
	if pw.pv.isTTY {
		pw.pv.clearLinesLocked()
	}
	pw.pv.mu.Unlock()
	return pw.w.Write(p)
}
