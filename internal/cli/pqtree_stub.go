//go:build !dev

package cli

import "github.com/spf13/cobra"

// pqtreeCommand is a no-op stub in release builds. The real implementation
// lives in pqtree.go behind the `dev` build tag so developer-only tooling
// never ships in public binaries. We still expose a hidden cobra command so
// RootCommand() wiring doesn't have to be conditional; it simply prints an
// "unknown command" message if someone types it.
//
// See pqtree.go for why this is gated.
func (c *CLI) pqtreeCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "pqtree",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return NewUserError(
				"`pqtree` is a developer-only command",
				"Rebuild with `go build -tags dev ./cmd/stacktower` to enable it.",
			)
		},
	}
}
