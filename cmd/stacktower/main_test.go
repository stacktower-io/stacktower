package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/matzehuels/stacktower/internal/cli"
)

func TestExitCodeForError(t *testing.T) {
	if got := cli.ExitCodeForError(nil); got != 0 {
		t.Fatalf("nil error exit code = %d, want %d", got, 0)
	}

	if got := cli.ExitCodeForError(context.Canceled); got != cli.ExitCodeInterrupted {
		t.Fatalf("context.Canceled exit code = %d, want %d", got, cli.ExitCodeInterrupted)
	}

	userErr := cli.NewUserError("invalid input", "run --help")
	if got := cli.ExitCodeForError(userErr); got != cli.ExitCodeUsage {
		t.Fatalf("user error exit code = %d, want %d", got, cli.ExitCodeUsage)
	}

	wrappedUser := fmt.Errorf("while parsing: %w", userErr)
	if got := cli.ExitCodeForError(wrappedUser); got != cli.ExitCodeUsage {
		t.Fatalf("wrapped user error exit code = %d, want %d", got, cli.ExitCodeUsage)
	}
}
