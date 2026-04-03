package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Spinner provides a simple progress indicator with context cancellation support.
// It uses shared animation frames from styles.go for consistent visual appearance
// across all CLI operations.
//
// For complex resolver operations that require observability hooks,
// use ProgressView instead.
type Spinner struct {
	message   string
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	stopped   chan struct{}
	active    bool
	mu        sync.Mutex
	closeOnce sync.Once
}

// NewSpinner creates a new spinner with the given message.
// The spinner uses shared animation frames from styles.go.
func NewSpinner(message string) *Spinner {
	return NewSpinnerWithContext(context.Background(), message)
}

// NewSpinnerWithContext creates a spinner that will stop when the context is cancelled.
// The spinner uses shared animation frames from styles.go.
func NewSpinnerWithContext(ctx context.Context, message string) *Spinner {
	spinnerCtx, cancel := context.WithCancel(ctx)
	return &Spinner{
		message: message,
		ctx:     spinnerCtx,
		cancel:  cancel,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start begins the spinner animation.
// In quiet mode, no animation is shown but Stop() remains callable.
func (s *Spinner) Start() {
	if quietMode {
		close(s.stopped)
		return
	}
	s.active = true
	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(SpinnerInterval)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-s.ctx.Done():
				s.clearLine()
				return
			case <-s.done:
				return
			case <-ticker.C:
				frame := SpinnerFrames[i%len(SpinnerFrames)]
				s.mu.Lock()
				fmt.Fprintf(os.Stderr, "\r%s %s", StyleIconSpinner.Render(frame), StyleDim.Render(s.message))
				s.mu.Unlock()
				i++
			}
		}
	}()
}

// Stop stops the spinner and clears the line.
// Stop is safe to call concurrently from multiple goroutines.
func (s *Spinner) Stop() {
	s.cancel()
	s.closeOnce.Do(func() { close(s.done) })
	<-s.stopped
	if s.active {
		s.clearLine()
	}
}

func (s *Spinner) clearLine() {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", len(s.message)+4))
}

// ClearLine clears the current spinner line without stopping.
// Use this before printing other output while the spinner is running.
func (s *Spinner) ClearLine() {
	s.clearLine()
}

// UpdateMessage changes the spinner message while running.
// This is useful for multi-phase operations that need to indicate progress.
func (s *Spinner) UpdateMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}

// StopWithSuccess stops the spinner and shows a success message.
func (s *Spinner) StopWithSuccess(message string) {
	s.Stop()
	PrintSuccess("%s", message)
}

// StopWithError stops the spinner and shows an error message.
func (s *Spinner) StopWithError(message string) {
	s.Stop()
	PrintError("%s", message)
}

// StopWithWarning stops the spinner and shows a warning message.
func (s *Spinner) StopWithWarning(message string) {
	s.Stop()
	PrintWarning("%s", message)
}

// Cancelled returns true if the spinner was stopped due to context cancellation.
func (s *Spinner) Cancelled() bool {
	return s.ctx.Err() != nil
}
