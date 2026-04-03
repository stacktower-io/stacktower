package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matzehuels/stacktower/internal/cli/ui"
	"github.com/matzehuels/stacktower/pkg/buildinfo"
	"github.com/matzehuels/stacktower/pkg/core/deps/languages"
)

func (c *CLI) infoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show supported languages, registries, and manifest files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printInfoDisplay()
		},
	}
}

func (c *CLI) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version and build information",
		RunE: func(cmd *cobra.Command, args []string) error {
			ui.PrintVersionInfo(buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			return nil
		},
	}
}

func printInfoDisplay() error {
	w := os.Stderr

	ui.PrintVersionInfo(buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	fmt.Fprintln(w)

	ui.PrintHeader("Supported Languages")

	for _, lang := range languages.All {
		fmt.Fprintf(w, "  %s  %s\n",
			ui.StyleHighlight.Render(lang.Name),
			ui.StyleDim.Render("registry: "+lang.DefaultRegistry))

		if len(lang.ManifestAliases) > 0 {
			filenames := make([]string, 0, len(lang.ManifestAliases))
			for f := range lang.ManifestAliases {
				filenames = append(filenames, f)
			}
			slices.Sort(filenames)

			fmt.Fprintf(w, "    %s %s\n",
				ui.StyleDim.Render("manifests:"),
				ui.StyleValue.Render(strings.Join(filenames, ", ")))
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s\n",
		ui.StyleDim.Render("Docs:"),
		ui.StyleLink.Render("https://app.stacktower.io/cli-docs"))

	return nil
}
