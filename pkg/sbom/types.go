// Package sbom generates standards-compliant Software Bill of Materials
// from Stacktower dependency graphs.
//
// # Supported Formats
//
//   - CycloneDX (JSON and XML, spec version 1.6)
//   - SPDX (JSON, spec version 2.3)
//
// # Usage
//
// Generate a CycloneDX SBOM from a parsed dependency graph:
//
//	opts := sbom.Options{
//	    Format:      sbom.FormatCycloneDX,
//	    Encoding:    sbom.EncodingJSON,
//	    Language:    "python",
//	    ToolName:    "stacktower",
//	    ToolVersion: "1.0.0",
//	}
//	data, err := sbom.GenerateCycloneDX(g, opts)
//
// Generate an SPDX SBOM:
//
//	opts := sbom.Options{
//	    Format:   sbom.FormatSPDX,
//	    Language: "python",
//	}
//	data, err := sbom.GenerateSPDX(g, opts)
//
// Both generators produce []byte output suitable for writing to a file or
// returning in an HTTP response. Package identifiers use Package URL (purl)
// format per the respective specifications.
package sbom

import "github.com/stacktower-io/stacktower/pkg/security"

// Format identifies the SBOM specification to generate.
type Format string

const (
	FormatCycloneDX Format = "cyclonedx"
	FormatSPDX      Format = "spdx"
)

// Encoding identifies the serialization format.
type Encoding string

const (
	EncodingJSON Encoding = "json"
	EncodingXML  Encoding = "xml"
)

// Options configures SBOM generation.
type Options struct {
	Format      Format
	Encoding    Encoding
	SpecVersion string           // e.g., "1.6" for CycloneDX, "2.3" for SPDX
	Language    string           // needed for purl construction
	ToolName    string           // e.g., "stacktower"
	ToolVersion string           // e.g., "1.2.0"
	VulnReport  *security.Report // optional vulnerability data
}
