package ui

import (
	"os"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// defaultTerminalWidth is the fallback width when we cannot detect the real
// terminal size (e.g. when stdout/stderr is piped or when ioctl fails).
// 80 is the long-standing default for non-TTY output and matches what most
// POSIX tools fall back to.
const defaultTerminalWidth = 80

// StdoutIsTTY reports whether stdout is an interactive terminal.
// Callers use this to decide between styled human output and machine-readable
// output suitable for pipes (e.g. `list | head`).
func StdoutIsTTY() bool {
	fd := os.Stdout.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// StderrIsTTY reports whether stderr is an interactive terminal. Progress
// views and spinners use this since they always write to stderr.
func StderrIsTTY() bool {
	fd := os.Stderr.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// StdoutWidth returns the current stdout terminal width, falling back to
// defaultTerminalWidth (80 columns) when stdout is not a terminal or the
// width cannot be determined.
func StdoutWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return defaultTerminalWidth
	}
	return w
}

// StderrWidth returns the current stderr terminal width, falling back to
// defaultTerminalWidth (80 columns). Used by the progress view to fit
// multi-line status output without wrapping.
func StderrWidth() int {
	w, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || w <= 0 {
		return defaultTerminalWidth
	}
	return w
}
