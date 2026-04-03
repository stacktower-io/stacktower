// Package security provides vulnerability scanning and license compliance
// checking for dependency graphs.
//
// This package defines the [Scanner] interface and data types for analyzing
// dependency trees for known vulnerabilities, as well as license risk
// classification for compliance checking. It provides a common vocabulary
// that CLI tools, API servers, and CI/CD integrations can use.
//
// # Architecture
//
// The package follows the same plugin pattern as [deps.MetadataProvider]:
//   - [Scanner] defines the vulnerability scanning contract
//   - Implementations wrap specific vulnerability databases (e.g., OSV.dev)
//   - Results are returned as a [Report] containing [Finding] entries
//   - [AnalyzeLicenses] classifies all dependencies by license risk
//   - [LicenseReport] summarises compliance status across the graph
//
// # Vulnerability Scanning
//
// Create a scanner and scan dependencies:
//
//	scanner := security.NewOSVScanner(nil)
//	deps := security.DependenciesFromGraph(graph, "python")
//	report, err := scanner.Scan(ctx, deps)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Found %d vulnerabilities\n", len(report.Findings))
//
// # License Compliance
//
// Analyse licenses from the dependency graph metadata:
//
//	report := security.AnalyzeLicenses(g) // reads "license" from node metadata
//	if !report.Compliant {
//	    fmt.Printf("Copyleft deps: %v\n", report.Copyleft)
//	    fmt.Printf("Unknown licenses: %v\n", report.Unknown)
//	}
//
// # Integration with Stacktower
//
// The security package is designed to work alongside the existing pipeline:
//   - After parsing a dependency graph, extract dependencies with [DependenciesFromGraph]
//   - Run the scanner to produce a [Report]
//   - Run [AnalyzeLicenses] to classify license risk and annotate nodes
//   - Both reports can be displayed in CLI output, rendered as visual indicators,
//     or stored alongside render documents
//
// [deps.MetadataProvider]: github.com/matzehuels/stacktower/pkg/core/deps.MetadataProvider
package security
