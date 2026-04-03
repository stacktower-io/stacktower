package cli

import (
	"context"
	"errors"
	"fmt"
)

const (
	// ExitCodeFailure is a generic runtime failure.
	ExitCodeFailure = 1
	// ExitCodeUsage indicates invalid input/usage from the caller.
	ExitCodeUsage = 2
	// ExitCodeInterrupted follows shell convention for SIGINT/SIGTERM.
	ExitCodeInterrupted = 130
)

// ErrorKind classifies CLI failures for exit-code mapping.
type ErrorKind string

const (
	ErrorKindUser   ErrorKind = "user"
	ErrorKindSystem ErrorKind = "system"
)

// CLIError is a structured CLI-facing error.
type CLIError struct {
	Kind    ErrorKind
	Message string
	Hint    string
	Cause   error
}

func (e *CLIError) Error() string {
	msg := e.Message
	if e.Hint != "" {
		msg += "\nHint: " + e.Hint
	}
	if e.Cause == nil {
		return msg
	}
	return fmt.Sprintf("%s: %v", msg, e.Cause)
}

func (e *CLIError) Unwrap() error {
	return e.Cause
}

// NewUserError constructs a usage/input error with an optional actionable hint.
func NewUserError(message string, hint string) error {
	return &CLIError{
		Kind:    ErrorKindUser,
		Message: message,
		Hint:    hint,
	}
}

// WrapUserError wraps a cause as a usage/input error.
func WrapUserError(cause error, message string, hint string) error {
	return &CLIError{
		Kind:    ErrorKindUser,
		Message: message,
		Hint:    hint,
		Cause:   cause,
	}
}

// NewSystemError constructs an execution/network/runtime error with optional hint.
func NewSystemError(message string, hint string) error {
	return &CLIError{
		Kind:    ErrorKindSystem,
		Message: message,
		Hint:    hint,
	}
}

// WrapSystemError wraps a cause as an execution/network/runtime error.
func WrapSystemError(cause error, message string, hint string) error {
	return &CLIError{
		Kind:    ErrorKindSystem,
		Message: message,
		Hint:    hint,
		Cause:   cause,
	}
}

// ExitCodeForError maps errors to stable process exit codes.
func ExitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return ExitCodeInterrupted
	}

	var cliErr *CLIError
	if errors.As(err, &cliErr) && cliErr.Kind == ErrorKindUser {
		return ExitCodeUsage
	}

	return ExitCodeFailure
}
