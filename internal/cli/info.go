package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/core/deps/languages"
)

// infoOutput is the structured form of `stacktower info -f json`.
type infoOutput struct {
	Version               versionOutput    `json:"version"`
	CompiledGitHubAppSlug string           `json:"compiled_github_app_slug"`
	CompiledGitHubAppURL  string           `json:"compiled_github_app_url,omitempty"`
	GitHubRepoURL         string           `json:"github_repo_url"`
	Languages             []languageOutput `json:"languages"`
	DocsURL               string           `json:"docs_url"`
}

type versionOutput struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

type languageOutput struct {
	Name      string   `json:"name"`
	Registry  string   `json:"registry"`
	Manifests []string `json:"manifests,omitempty"`
}

const docsURL = "https://app.stacktower.io/cli-docs"
const githubRepoURL = "https://github.com/stacktower-io/stacktower"

func githubAppURLFromSlug(slug string) string {
	if slug == "" {
		return ""
	}
	return "https://github.com/apps/" + slug
}

func (c *CLI) infoCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show supported languages, registries, and manifest files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printInfo(format)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: text (default), json")
	return cmd
}

func (c *CLI) versionCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version and build information",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printVersion(format)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: text (default), json")
	return cmd
}

// buildInfoOutput builds the structured info payload from the live language registry.
func buildInfoOutput() infoOutput {
	out := infoOutput{
		Version: versionOutput{
			Version: buildinfo.Version,
			Commit:  buildinfo.Commit,
			Date:    buildinfo.Date,
		},
		CompiledGitHubAppSlug: buildinfo.CompiledGitHubAppSlug,
		CompiledGitHubAppURL:  githubAppURLFromSlug(buildinfo.CompiledGitHubAppSlug),
		GitHubRepoURL:         githubRepoURL,
		DocsURL:               docsURL,
	}
	for _, lang := range languages.All {
		entry := languageOutput{Name: lang.Name, Registry: lang.DefaultRegistry}
		if len(lang.ManifestAliases) > 0 {
			entry.Manifests = make([]string, 0, len(lang.ManifestAliases))
			for f := range lang.ManifestAliases {
				entry.Manifests = append(entry.Manifests, f)
			}
			slices.Sort(entry.Manifests)
		}
		out.Languages = append(out.Languages, entry)
	}
	return out
}

func printInfo(format string) error {
	switch format {
	case FormatJSON:
		return encodeJSON(os.Stdout, buildInfoOutput())
	case "", FormatText:
		return printInfoDisplay()
	default:
		return unsupportedFormatError(format, nil)
	}
}

func printVersion(format string) error {
	switch format {
	case FormatJSON:
		return encodeJSON(os.Stdout, versionOutput{
			Version: buildinfo.Version,
			Commit:  buildinfo.Commit,
			Date:    buildinfo.Date,
		})
	case "", FormatText:
		ui.PrintVersionInfo(buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	default:
		return unsupportedFormatError(format, nil)
	}
}

// printInfoDisplay renders the human-readable `info` output.
//
// NOTE: this intentionally writes to stderr while the JSON branch in the
// caller writes to stdout. That asymmetry is deliberate: text mode is UI
// ("here's what this binary supports"), so it belongs on stderr where it
// won't pollute a pipe; JSON mode is data, so it goes to stdout where it
// can be piped into jq/other tooling. Please don't "fix" this by routing
// both to the same stream.
func printInfoDisplay() error {
	w := os.Stderr
	labelPad := 12

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
	ui.PrintSeparator("Links")
	githubAppSlug := buildinfo.CompiledGitHubAppSlug
	if githubAppSlug == "" {
		fmt.Fprintf(w, "  %s %s\n",
			ui.StyleDim.Render(fmt.Sprintf("%-*s", labelPad, "GitHub App:")),
			ui.StyleValue.Render("(not set)"))
	} else {
		fmt.Fprintf(w, "  %s %s\n",
			ui.StyleDim.Render(fmt.Sprintf("%-*s", labelPad, "GitHub App:")),
			ui.StyleLink.Render(githubAppURLFromSlug(githubAppSlug)))
	}
	fmt.Fprintf(w, "  %s %s\n",
		ui.StyleDim.Render(fmt.Sprintf("%-*s", labelPad, "GitHub Repo:")),
		ui.StyleLink.Render(githubRepoURL))
	fmt.Fprintf(w, "  %s %s\n",
		ui.StyleDim.Render(fmt.Sprintf("%-*s", labelPad, "Docs:")),
		ui.StyleLink.Render(docsURL))
	fmt.Fprintln(w)

	return nil
}
