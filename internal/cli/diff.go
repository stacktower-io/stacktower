package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

func (c *CLI) diffCommand() *cobra.Command {
	var (
		format    string
		output    string
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

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (stdout if empty)")
	cmd.Flags().BoolVar(&failOnVuln, "fail-on-vuln", false, "Exit 3 if new vulnerabilities were introduced")

	return cmd
}

type diffJSON struct {
	Before    diffSideJSON   `json:"before"`
	After     diffSideJSON   `json:"after"`
	Added     []diffEntryJSON  `json:"added"`
	Removed   []diffEntryJSON  `json:"removed"`
	Updated   []diffUpdateJSON `json:"updated"`
	Unchanged int              `json:"unchanged"`
	NewVulns  []diffVulnJSON   `json:"new_vulns"`
}

type diffSideJSON struct {
	Root    string `json:"root"`
	Version string `json:"version"`
	Total   int    `json:"total"`
}

type diffEntryJSON struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

type diffUpdateJSON struct {
	Package    string `json:"package"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
}

type diffVulnJSON struct {
	Package  string `json:"package"`
	Severity string `json:"severity"`
	Version  string `json:"version"`
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
	before, err := loadGraph(beforeInput)
	if err != nil {
		return WrapSystemError(err, "failed to load 'before' graph", "")
	}
	after, err := loadGraph(afterInput)
	if err != nil {
		return WrapSystemError(err, "failed to load 'after' graph", "")
	}

	d := dag.Diff(before, after)

	w := os.Stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return WrapSystemError(err, "failed to create output file", "")
		}
		defer f.Close()
		w = f
	}

	switch format {
	case "json":
		if err := writeDiffJSON(w, d); err != nil {
			return WrapSystemError(err, "failed to write JSON output", "")
		}
	default:
		ui.WriteDiff(w, d)
	}

	if failOnVuln && len(d.NewVulns) > 0 {
		return &VulnError{Count: len(d.NewVulns)}
	}

	return nil
}

func writeDiffJSON(w *os.File, d *dag.DiffResult) error {
	out := diffJSON{
		Before: diffSideJSON{
			Root:    d.Before.RootID,
			Version: d.Before.RootVersion,
			Total:   d.Before.NodeCount,
		},
		After: diffSideJSON{
			Root:    d.After.RootID,
			Version: d.After.RootVersion,
			Total:   d.After.NodeCount,
		},
		Unchanged: d.Unchanged,
	}

	for _, e := range d.Added {
		out.Added = append(out.Added, diffEntryJSON{Package: e.ID, Version: e.Version})
	}
	for _, e := range d.Removed {
		out.Removed = append(out.Removed, diffEntryJSON{Package: e.ID, Version: e.Version})
	}
	for _, u := range d.Updated {
		out.Updated = append(out.Updated, diffUpdateJSON{
			Package:    u.ID,
			OldVersion: u.OldVersion,
			NewVersion: u.NewVersion,
		})
	}
	for _, v := range d.NewVulns {
		out.NewVulns = append(out.NewVulns, diffVulnJSON{
			Package:  v.ID,
			Severity: v.Severity,
			Version:  v.Version,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
