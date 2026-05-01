package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/graph"
)

// Format constants for the "text | json" output switch used by analysis commands
// (why, stats, diff, sbom). Centralizing these keeps help text and default
// values aligned across commands.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// unsupportedFormatError returns a CLIError for an unknown --format value.
// When called from writeFormatted, the writers map keys are used for the hint;
// other callers pass nil (falls back to the default text/json hint).
func unsupportedFormatError(got string, writers map[string]func(io.Writer) error) error {
	hint := "Supported formats: text, json"
	if len(writers) > 0 {
		keys := make([]string, 0, len(writers))
		for k := range writers {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		hint = "Supported formats: " + strings.Join(keys, ", ")
	}
	return NewUserError(fmt.Sprintf("unsupported format: %q", got), hint)
}

// fileMode is the default file permission for CLI-written output files.
const fileMode os.FileMode = 0o644

// loadGraph reads a dependency graph from a file path or stdin (when input is "-").
// This is the shared entry point used by why, stats, diff, sbom, and render.
func loadGraph(input string) (*dag.DAG, error) {
	if input == "-" {
		return graph.ReadGraph(os.Stdin)
	}
	return graph.ReadGraphFile(input)
}

// writeFile writes raw data to the specified path (or stdout if path is empty).
// Uses fileMode for permissions on newly created files.
func writeFile(data []byte, path string) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, fileMode)
}

// openOutput returns a writer for the given path. If path is empty, it returns
// os.Stdout and a no-op closer. Otherwise it creates the file and returns a
// closer that flushes and closes it, surfacing the close error so that
// truncated JSON (e.g. disk-full) is not silently swallowed.
func openOutput(path string) (io.Writer, func() error, error) {
	if path == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return nil, nil, WrapSystemError(err, "failed to create output file",
			"Check that the output path is writable.")
	}
	return f, f.Close, nil
}

// writeFormatted routes output to path (or stdout) with a format-specific
// writer. Each command provides a map of format name to writer function; if
// format is not present, the command's default writer is used.
//
// The close error from openOutput is surfaced, which catches partial writes
// from disk-full, network filesystem errors, etc.
func writeFormatted(path, format string, writers map[string]func(io.Writer) error) error {
	fn, ok := writers[format]
	if !ok {
		return unsupportedFormatError(format, writers)
	}

	w, closer, err := openOutput(path)
	if err != nil {
		return err
	}
	if werr := fn(w); werr != nil {
		_ = closer()
		return WrapSystemError(werr, "failed to write output",
			"Check disk space, permissions, and the output path.")
	}
	if cerr := closer(); cerr != nil {
		return WrapSystemError(cerr, "failed to flush output file",
			"The file may be incomplete. Check disk space and permissions.")
	}
	return nil
}

// encodeJSON writes v to w as indented JSON.
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
