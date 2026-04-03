package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli"
	"github.com/matzehuels/stacktower/internal/cli/ui"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, ui.StyleError.Render("Error:")+fmt.Sprintf(" %s", err))
		os.Exit(cli.ExitCodeForError(err))
	}
}

func run(ctx context.Context) error {
	var (
		verbose bool
		quiet   bool
	)

	c := cli.New(os.Stderr, cli.LogInfo)
	root := c.RootCommand()

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")

	originalPreRun := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		level := cli.LogInfo
		if verbose {
			level = cli.LogDebug
		}
		c.SetLogLevel(level)
		c.SetQuiet(quiet)

		if originalPreRun != nil {
			return originalPreRun(cmd, args)
		}
		return nil
	}

	return root.ExecuteContext(ctx)
}
