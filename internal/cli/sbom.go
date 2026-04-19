package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/stacktower-io/stacktower/pkg/buildinfo"
	"github.com/stacktower-io/stacktower/pkg/sbom"
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

	cmd.Flags().StringVarP(&format, "format", "f", "cyclonedx", "SBOM format: cyclonedx, spdx")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (stdout if empty)")
	cmd.Flags().StringVar(&encoding, "encoding", "json", "Serialization: json, xml (CycloneDX only)")
	cmd.Flags().StringVar(&specVersion, "spec-version", "", "Specification version (default: latest supported)")

	return cmd
}

func (c *CLI) runSBOM(input, format, output, encoding, specVersion string) error {
	g, err := loadGraph(input)
	if err != nil {
		return WrapSystemError(err, "failed to load graph", "")
	}

	opts := sbom.Options{
		Format:      sbom.Format(format),
		Encoding:    sbom.Encoding(encoding),
		SpecVersion: specVersion,
		ToolName:    "stacktower",
		ToolVersion: buildinfo.Version,
	}

	if lang, ok := g.Meta()["language"].(string); ok {
		opts.Language = lang
	}

	var data []byte
	switch opts.Format {
	case sbom.FormatSPDX:
		data, err = sbom.GenerateSPDX(g, opts)
	default:
		data, err = sbom.GenerateCycloneDX(g, opts)
	}
	if err != nil {
		return WrapSystemError(err, "failed to generate SBOM", "")
	}

	if output != "" {
		if err := os.WriteFile(output, data, 0644); err != nil {
			return WrapSystemError(err, "failed to write SBOM file", "")
		}
		return nil
	}

	_, err = os.Stdout.Write(data)
	if err != nil {
		return WrapSystemError(err, "failed to write SBOM to stdout", "")
	}
	// Ensure trailing newline for terminal output
	if len(data) > 0 && data[len(data)-1] != '\n' {
		os.Stdout.Write([]byte("\n"))
	}
	return nil
}
