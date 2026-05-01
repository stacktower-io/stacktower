package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
)

// =============================================================================
// CLI Observability Hook Implementations
// =============================================================================

// cliPipelineHooks logs pipeline stage transitions and, when a spinner is
// attached, relays ordering progress to the CLI spinner so the user sees
// live counts during long optimal-ordering searches.
type cliPipelineHooks struct {
	logger          *log.Logger
	spinnerMu       sync.RWMutex
	orderingSpinner *ui.Spinner
}

// SetOrderingSpinner attaches (or detaches, with nil) a spinner that will
// receive live ordering-progress updates. Safe for concurrent use.
func (h *cliPipelineHooks) SetOrderingSpinner(s *ui.Spinner) {
	h.spinnerMu.Lock()
	defer h.spinnerMu.Unlock()
	h.orderingSpinner = s
}

func (h *cliPipelineHooks) OnParseStart(_ context.Context, language, pkg string) {
	h.logger.Debug("parse starting", "language", language, "package", pkg)
}

func (h *cliPipelineHooks) OnParseComplete(_ context.Context, language, pkg string, nodes int, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("parse failed", "language", language, "package", pkg, "duration", d, "err", err)
	} else {
		h.logger.Debug("parse complete", "language", language, "package", pkg, "nodes", nodes, "duration", d)
	}
}

func (h *cliPipelineHooks) OnLayoutStart(_ context.Context, vizType string, nodes int) {
	h.logger.Debug("layout starting", "type", vizType, "nodes", nodes)
}

func (h *cliPipelineHooks) OnLayoutComplete(_ context.Context, vizType string, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("layout failed", "type", vizType, "duration", d, "err", err)
	} else {
		h.logger.Debug("layout complete", "type", vizType, "duration", d)
	}
}

func (h *cliPipelineHooks) OnOrderingStart(_ context.Context, algorithm string, rowCount int) {
	h.logger.Debug("ordering starting", "algorithm", algorithm, "rows", rowCount)
}

func (h *cliPipelineHooks) OnOrderingProgress(_ context.Context, explored, pruned, bestCrossings int) {
	h.logger.Debug("ordering progress", "explored", explored, "pruned", pruned, "best_crossings", bestCrossings)
	if bestCrossings < 0 {
		return
	}
	h.spinnerMu.RLock()
	sp := h.orderingSpinner
	h.spinnerMu.RUnlock()
	if sp == nil {
		return
	}
	sp.UpdateMessage(fmt.Sprintf("Ordering... %s explored, best: %d crossings",
		ui.FormatCount(explored+pruned), bestCrossings))
}

func (h *cliPipelineHooks) OnOrderingComplete(_ context.Context, crossings int, d time.Duration) {
	h.logger.Debug("ordering complete", "crossings", crossings, "duration", d)
}

func (h *cliPipelineHooks) OnRenderStart(_ context.Context, formats []string) {
	h.logger.Debug("render starting", "formats", formats)
}

func (h *cliPipelineHooks) OnRenderComplete(_ context.Context, formats []string, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("render failed", "formats", formats, "duration", d, "err", err)
	} else {
		h.logger.Debug("render complete", "formats", formats, "duration", d)
	}
}

// cliSecurityHooks provides user-facing feedback during vulnerability scanning.
type cliSecurityHooks struct {
	logger *log.Logger
}

func (h *cliSecurityHooks) OnScanStart(_ context.Context, ecosystem string, depCount int) {
	ui.PrintInfo("Scanning %d %s dependencies for vulnerabilities...", depCount, ecosystem)
}

func (h *cliSecurityHooks) OnScanComplete(_ context.Context, ecosystem string, findings int, d time.Duration, err error) {
	if err != nil {
		h.logger.Debug("security scan failed", "ecosystem", ecosystem, "duration", d, "err", err)
	} else if findings > 0 {
		ui.PrintWarning("Found %d vulnerabilities (%s)", findings, ui.FormatDuration(d))
	} else {
		ui.PrintInfo("No known vulnerabilities found (%s)", ui.FormatDuration(d))
	}
}
