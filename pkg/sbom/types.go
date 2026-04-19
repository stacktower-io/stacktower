// Package sbom generates standards-compliant Software Bill of Materials
// from Stacktower dependency graphs.
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
