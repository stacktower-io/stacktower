package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		// Nil error
		{"nil error returns 0", nil, 0},

		// Context cancellation (SIGINT/SIGTERM)
		{"context.Canceled returns 130", context.Canceled, ExitCodeInterrupted},
		{"wrapped context.Canceled returns 130", fmt.Errorf("operation: %w", context.Canceled), ExitCodeInterrupted},
		{"double-wrapped context.Canceled returns 130", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", context.Canceled)), ExitCodeInterrupted},

		// User errors (exit code 2)
		{"CLIError user kind returns 2", NewUserError("invalid input", ""), ExitCodeUsage},
		{"wrapped CLIError user kind returns 2", WrapUserError(errors.New("cause"), "invalid input", ""), ExitCodeUsage},

		// System errors (exit code 1)
		{"CLIError system kind returns 1", NewSystemError("network failed", ""), ExitCodeFailure},
		{"wrapped CLIError system kind returns 1", WrapSystemError(errors.New("cause"), "network failed", ""), ExitCodeFailure},

		// Plain errors (exit code 1)
		{"plain error returns 1", errors.New("something went wrong"), ExitCodeFailure},
		{"fmt.Errorf returns 1", fmt.Errorf("formatted error"), ExitCodeFailure},

		// Edge cases
		{"context.DeadlineExceeded returns 1 (not 130)", context.DeadlineExceeded, ExitCodeFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitCodeForError(tt.err)
			if got != tt.wantCode {
				t.Errorf("ExitCodeForError() = %d, want %d", got, tt.wantCode)
			}
		})
	}
}

func TestCLIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *CLIError
		wantMsg  string
		contains []string
	}{
		{
			name:    "message only",
			err:     &CLIError{Kind: ErrorKindUser, Message: "invalid package"},
			wantMsg: "invalid package",
		},
		{
			name:     "message with hint",
			err:      &CLIError{Kind: ErrorKindUser, Message: "invalid package", Hint: "Check the package name"},
			contains: []string{"invalid package", "Hint:", "Check the package name"},
		},
		{
			name:     "message with cause",
			err:      &CLIError{Kind: ErrorKindSystem, Message: "network error", Cause: errors.New("connection refused")},
			contains: []string{"network error", "connection refused"},
		},
		{
			name:     "message with hint and cause",
			err:      &CLIError{Kind: ErrorKindSystem, Message: "fetch failed", Hint: "Check connectivity", Cause: errors.New("timeout")},
			contains: []string{"fetch failed", "Hint:", "Check connectivity", "timeout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if tt.wantMsg != "" && got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
			for _, s := range tt.contains {
				if !containsString(got, s) {
					t.Errorf("Error() = %q, should contain %q", got, s)
				}
			}
		})
	}
}

func TestCLIError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := WrapUserError(cause, "wrapper", "hint")

	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected error to be CLIError")
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the wrapped cause")
	}

	unwrapped := cliErr.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Test nil cause
	errNoCause := NewUserError("no cause", "")
	var cliErrNoCause *CLIError
	if errors.As(errNoCause, &cliErrNoCause) {
		if cliErrNoCause.Unwrap() != nil {
			t.Error("Unwrap() should return nil when no cause")
		}
	}
}

func TestNewUserError(t *testing.T) {
	err := NewUserError("test message", "test hint")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}

	if cliErr.Kind != ErrorKindUser {
		t.Errorf("Kind = %v, want %v", cliErr.Kind, ErrorKindUser)
	}
	if cliErr.Message != "test message" {
		t.Errorf("Message = %q, want %q", cliErr.Message, "test message")
	}
	if cliErr.Hint != "test hint" {
		t.Errorf("Hint = %q, want %q", cliErr.Hint, "test hint")
	}
	if cliErr.Cause != nil {
		t.Error("Cause should be nil")
	}
}

func TestWrapUserError(t *testing.T) {
	cause := errors.New("underlying error")
	err := WrapUserError(cause, "test message", "test hint")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}

	if cliErr.Kind != ErrorKindUser {
		t.Errorf("Kind = %v, want %v", cliErr.Kind, ErrorKindUser)
	}
	if cliErr.Cause != cause {
		t.Error("Cause should be the wrapped error")
	}
}

func TestNewSystemError(t *testing.T) {
	err := NewSystemError("system failure", "retry later")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}

	if cliErr.Kind != ErrorKindSystem {
		t.Errorf("Kind = %v, want %v", cliErr.Kind, ErrorKindSystem)
	}
}

func TestWrapSystemError(t *testing.T) {
	cause := errors.New("network timeout")
	err := WrapSystemError(cause, "request failed", "check network")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatal("expected CLIError")
	}

	if cliErr.Kind != ErrorKindSystem {
		t.Errorf("Kind = %v, want %v", cliErr.Kind, ErrorKindSystem)
	}
	if cliErr.Cause != cause {
		t.Error("Cause should be the wrapped error")
	}
}

func TestExitCodeConstants(t *testing.T) {
	// Verify exit codes match POSIX conventions
	if ExitCodeFailure != 1 {
		t.Errorf("ExitCodeFailure = %d, want 1", ExitCodeFailure)
	}
	if ExitCodeUsage != 2 {
		t.Errorf("ExitCodeUsage = %d, want 2", ExitCodeUsage)
	}
	if ExitCodeInterrupted != 130 {
		t.Errorf("ExitCodeInterrupted = %d, want 130 (128 + SIGINT)", ExitCodeInterrupted)
	}
}

func TestErrorKindConstants(t *testing.T) {
	// Verify error kinds are distinct
	if ErrorKindUser == ErrorKindSystem {
		t.Error("ErrorKindUser and ErrorKindSystem should be different")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
