// Package errors provides structured error types for the Stacktower application.
//
// This package defines error codes and types that enable:
//   - Consistent error handling across API and library packages
//   - Machine-readable error codes for programmatic handling
//   - User-friendly error messages
//   - Error wrapping with context preservation
//
// # Error Codes
//
// Error codes follow a hierarchical naming convention:
//   - INVALID_*: Input validation failures
//   - NOT_FOUND_*: Resource not found
//   - NETWORK_*: Network-related errors
//   - INTERNAL_*: Unexpected internal errors
//
// # Usage
//
//	err := errors.New(errors.ErrCodeInvalidInput, "invalid package name: %s", name)
//	if errors.Is(err, errors.ErrCodeInvalidInput) {
//	    // Handle validation error
//	}
//
//	// Wrap existing errors
//	err := errors.Wrap(errors.ErrCodeNetwork, origErr, "failed to fetch %s", url)
package errors

import (
	"errors"
	"fmt"
)

// Code represents a machine-readable error code.
type Code string

// Error codes for different error categories.
const (
	// Input validation errors
	ErrCodeInvalidInput    Code = "INVALID_INPUT"
	ErrCodeInvalidLanguage Code = "INVALID_LANGUAGE"
	ErrCodeInvalidPackage  Code = "INVALID_PACKAGE"
	ErrCodeInvalidFormat   Code = "INVALID_FORMAT"
	ErrCodeInvalidStyle    Code = "INVALID_STYLE"
	ErrCodeInvalidVizType  Code = "INVALID_VIZ_TYPE"
	ErrCodeInvalidManifest Code = "INVALID_MANIFEST"
	ErrCodeInvalidPath     Code = "INVALID_PATH"

	// Resource not found errors
	ErrCodeNotFound        Code = "NOT_FOUND"
	ErrCodePackageNotFound Code = "PACKAGE_NOT_FOUND"
	ErrCodeFileNotFound    Code = "FILE_NOT_FOUND"
	ErrCodeSessionNotFound Code = "SESSION_NOT_FOUND"

	// Network errors
	ErrCodeNetwork     Code = "NETWORK_ERROR"
	ErrCodeTimeout     Code = "TIMEOUT"
	ErrCodeRateLimited Code = "RATE_LIMITED"

	// Authentication errors
	ErrCodeUnauthorized   Code = "UNAUTHORIZED"
	ErrCodeForbidden      Code = "FORBIDDEN"
	ErrCodeSessionExpired Code = "SESSION_EXPIRED"

	// Internal errors
	ErrCodeInternal    Code = "INTERNAL_ERROR"
	ErrCodeUnsupported Code = "UNSUPPORTED"
)

// Error is a structured error with a code and optional cause.
type Error struct {
	Code    Code   // Machine-readable error code
	Message string // Human-readable message
	Cause   error  // Underlying error (optional)
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for errors.Is/As compatibility.
func (e *Error) Unwrap() error {
	return e.Cause
}

// New creates a new Error with the given code and formatted message.
func New(code Code, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap creates a new Error wrapping an existing error.
func Wrap(code Code, cause error, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// Is reports whether err has the given error code.
// It unwraps the error chain looking for an *Error with a matching code.
func Is(err error, code Code) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

// GetCode extracts the error code from an error, if available.
// Returns empty string if the error is not an *Error.
func GetCode(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// UserMessage returns a user-friendly message for the error.
// For *Error types, returns the message without the code prefix.
// For other errors, returns the error string as-is.
func UserMessage(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

// RateLimitedError provides additional information for rate-limited responses.
type RateLimitedError struct {
	RetryAfter int // Seconds to wait before retrying
	Message    string
}

// Error implements the error interface.
func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited: retry after %d seconds", e.RetryAfter)
	}
	return "rate limited"
}

// Code returns the error code for this error type.
func (e *RateLimitedError) Code() Code {
	return ErrCodeRateLimited
}
