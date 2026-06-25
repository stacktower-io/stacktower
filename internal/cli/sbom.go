package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/internal/cli/ui"
	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/sbom"
	"github.com/stacktower-io/stacktower/pkg/security"
)

func (c *CLI) sbomCommand() *cobra.Command {
	var (
		format      string
		output      string
		encoding    string
		specVersion string
	)

	cmd := &cobra.Command{
		Use:   "sbom [graph.json|-]",
		Short: "Export dependency graph as SBOM",
		Long: `Export the parsed dependency graph as a standards-compliant Software Bill of
Materials in CycloneDX or SPDX format.

This makes Stacktower useful in compliance workflows. The SBOM includes package
identifiers (purls), license data, dependency relationships, and optionally
vulnerability findings when the graph was parsed with --security-scan.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runSBOM(args[0], format, output, encoding, specVersion)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", string(sbom.FormatCycloneDX), "SBOM format: cyclonedx, spdx")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (stdout if omitted)")
	cmd.Flags().StringVar(&encoding, "encoding", string(sbom.EncodingJSON), "serialization: json, xml (CycloneDX only)")
	cmd.Flags().StringVar(&specVersion, "spec-version", "", "specification version (default: latest supported)")

	return cmd
}

func (c *CLI) runSBOM(input, format, output, encoding, specVersion string) error {
	g, err := loadGraph(input)
	if err != nil {
		return WrapSystemError(err, "failed to load graph", "Check that the file exists and contains valid graph JSON.")
	}

	sbomFmt := sbom.Format(format)
	switch sbomFmt {
	case sbom.FormatCycloneDX, sbom.FormatSPDX:
	default:
		return NewUserError(
			fmt.Sprintf("unsupported SBOM format: %q", format),
			"Supported formats: cyclonedx, spdx",
		)
	}

	sbomEncoding := sbom.Encoding(encoding)
	switch sbomEncoding {
	case sbom.EncodingJSON, sbom.EncodingXML:
	default:
		return NewUserError(
			fmt.Sprintf("unsupported SBOM encoding: %q", encoding),
			"Supported encodings: json, xml",
		)
	}
	if sbomFmt == sbom.FormatSPDX && sbomEncoding == sbom.EncodingXML {
		return NewUserError(
			"SPDX SBOM output does not support XML encoding",
			"Use --encoding json for SPDX, or --format cyclonedx --encoding xml.",
		)
	}

	if specVersion != "" {
		switch sbomFmt {
		case sbom.FormatCycloneDX:
			if specVersion != "1.5" && specVersion != "1.6" {
				return NewUserError(
					fmt.Sprintf("unsupported CycloneDX spec version: %q", specVersion),
					"Supported versions: 1.5, 1.6 (default).",
				)
			}
		case sbom.FormatSPDX:
			if specVersion != "2.3" {
				return NewUserError(
					fmt.Sprintf("unsupported SPDX spec version: %q", specVersion),
					"SPDX output always uses version 2.3; omit --spec-version or pass 2.3.",
				)
			}
		}
	}

	opts := sbom.Options{
		Format:      sbomFmt,
		Encoding:    sbomEncoding,
		SpecVersion: specVersion,
		ToolName:    "stacktower",
		ToolVersion: buildinfo.Version,
	}

	if lang, ok := g.Meta()["language"].(string); ok {
		opts.Language = lang
	}

	// Recover full vulnerability findings stored during --security-scan so
	// the SBOM carries real OSV/GHSA identifiers.
	opts.VulnReport = security.ReportFromMeta(g)

	var data []byte
	switch sbomFmt {
	case sbom.FormatSPDX:
		data, err = sbom.GenerateSPDX(g, opts)
	default:
		data, err = sbom.GenerateCycloneDX(g, opts)
	}
	if err != nil {
		return WrapSystemError(err, "failed to generate SBOM", "Check that the graph JSON is valid and was produced by 'stacktower parse'.")
	}

	if err := writeFile(data, output); err != nil {
		return WrapSystemError(err, "failed to write SBOM", "Check that the output path is writable.")
	}

	if output != "" {
		ui.PrintNewline()
		ui.PrintSuccess("SBOM written (%s)", sbomFmt)
		ui.PrintFile(output)
	}
	return nil
}
