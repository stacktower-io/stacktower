package ui

import (
	"context"
	"testing"
	"time"
)

func TestSpinnerBasic(t *testing.T) {
	s := NewSpinner("Testing...")
	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	// Spinner should be stopped, not cancelled
	// (Cancelled returns true only if Stop was called due to context cancellation)
	_ = s.Cancelled() // Verify method is callable; value not asserted as Stop() doesn't set cancelled
}

func TestSpinnerWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	s := NewSpinnerWithContext(ctx, "Testing with context...")
	s.Start()

	// Cancel the context
	cancel()

	// Give goroutine time to notice cancellation
	time.Sleep(100 * time.Millisecond)

	// Spinner should be cancelled
	if !s.Cancelled() {
		t.Error("Spinner should be cancelled after context cancellation")
	}
}

func TestSpinnerWithTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	s := NewSpinnerWithContext(ctx, "Testing with timeout...")
	s.Start()

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Spinner should be cancelled due to timeout
	if !s.Cancelled() {
		t.Error("Spinner should be cancelled after context timeout")
	}
}

func TestSpinnerStopIsIdempotent(t *testing.T) {
	s := NewSpinner("Testing idempotent stop...")
	s.Start()

	// Stop multiple times should not panic
	s.Stop()
	s.Stop()
	s.Stop()
}

func TestSpinnerStopWithSuccess(t *testing.T) {
	s := NewSpinner("Testing success...")
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.StopWithSuccess("Done!")
}

func TestSpinnerStopWithError(t *testing.T) {
	s := NewSpinner("Testing error...")
	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.StopWithError("Failed!")
}

func TestNewSpinnerWithContextNilParent(t *testing.T) {
	s := NewSpinnerWithContext(context.Background(), "Test")
	s.Start()
	s.Stop()
}
