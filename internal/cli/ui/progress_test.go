package ui

import (
	"context"
	"testing"
)

func TestFitToWidth_NoLimitOrFits(t *testing.T) {
	if got := FitToWidth("hello", 0); got != "hello" {
		t.Fatalf("FitToWidth no limit = %q, want %q", got, "hello")
	}
	if got := FitToWidth("hello", 5); got != "hello" {
		t.Fatalf("FitToWidth exact fit = %q, want %q", got, "hello")
	}
}

func TestFitToWidth_TruncatesWithEllipsis(t *testing.T) {
	got := FitToWidth("hello world", 8)
	if got != "hello w…" {
		t.Fatalf("FitToWidth truncated = %q, want %q", got, "hello w…")
	}
}

func TestFitToWidth_VeryNarrow(t *testing.T) {
	if got := FitToWidth("hello", 1); got != "" {
		t.Fatalf("FitToWidth width=1 = %q, want empty", got)
	}
}

func TestProgressStartRespectsQuietMode(t *testing.T) {
	prev := quietMode
	quietMode = true
	defer func() { quietMode = prev }()

	pv := NewProgressView(context.Background(), "Resolving...", 10)
	pv.Start()
	if pv.active {
		t.Fatalf("progress view should remain inactive in quiet mode")
	}
}

func TestProgressStartStopLifecycle(t *testing.T) {
	prev := quietMode
	quietMode = false
	defer func() { quietMode = prev }()

	pv := NewProgressView(context.Background(), "Resolving...", 10)
	pv.Start()
	if !pv.active {
		t.Fatalf("progress view should become active after Start")
	}

	pv.Stop()
	if pv.active {
		t.Fatalf("progress view should be inactive after Stop")
	}
}
