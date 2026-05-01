package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func (c *CLI) diffCommand() *cobra.Command {
	var (
		format     string
		output     string
		failOnVuln bool
	)

	cmd := &cobra.Command{
		Use:   "diff [before.json] [after.json|-]",
		Short: "Compare two dependency graphs",
		Long: `Compare two dependency graphs and report what changed: added, removed,
updated, and changed-depth packages. Optionally fail if new vulnerabilities
were introduced (useful in CI pipelines).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runDiff(args[0], args[1], format, output, failOnVuln)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", FormatText, "output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (stdout if omitted)")
	cmd.Flags().BoolVar(&failOnVuln, "fail-on-vuln", false, "exit 3 if new vulnerabilities were introduced")

	return cmd
}

// VulnError is returned when --fail-on-vuln detects new vulnerabilities.
// It maps to ExitCodeVuln (3) via ExitCodeForError.
type VulnError struct {
	Count int
}

func (e *VulnError) Error() string {
	return fmt.Sprintf("%d new vulnerabilities detected", e.Count)
}

func (c *CLI) runDiff(beforeInput, afterInput, format, output string, failOnVuln bool) error {
	if beforeInput == "-" && afterInput == "-" {
		return NewUserError(
			"diff cannot read both graphs from stdin",
			"Pass one graph as a file path, or write one side to a temporary file before diffing.",
		)
	}

	before, err := loadGraph(beforeInput)
	if err != nil {
		return WrapSystemError(err, "failed to load 'before' graph", "Check that the file exists and contains valid graph JSON.")
	}
	after, err := loadGraph(afterInput)
	if err != nil {
		return WrapSystemError(err, "failed to load 'after' graph", "Check that the file exists and contains valid graph JSON.")
	}

	d := dag.Diff(before, after)

	writers := map[string]func(io.Writer) error{
		FormatJSON: func(w io.Writer) error { return writeDiffJSON(w, d) },
		FormatText: func(w io.Writer) error { ui.WriteDiff(w, d); return nil },
	}
	if err := writeFormatted(output, format, writers); err != nil {
		return err
	}

	if output != "" {
		ui.PrintNewline()
		ui.PrintSuccess("Diff written")
		ui.PrintFile(output)
	}

	if failOnVuln && len(d.NewVulns) > 0 {
		return &VulnError{Count: len(d.NewVulns)}
	}

	return nil
}

// writeDiffJSON encodes the canonical dag.DiffResult directly. JSON tags on
// the canonical type keep the external schema stable while avoiding a
// shadow DTO that would have to be kept in sync. DepthChange on DiffUpdate
// uses omitempty, so zero values are silently elided and scripts that
// predate its introduction continue to see the same keys.
func writeDiffJSON(w io.Writer, d *dag.DiffResult) error {
	return encodeJSON(w, d)
}
