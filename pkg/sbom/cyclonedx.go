package sbom

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// CycloneDX BOM types (CycloneDX 1.6 JSON/XML schema)

type cdxBOM struct {
	XMLName      xml.Name        `json:"-" xml:"bom"`
	BOMFormat    string          `json:"bomFormat" xml:"-"`
	SpecVersion  string          `json:"specVersion" xml:"version,attr"`
	SerialNumber string          `json:"serialNumber" xml:"serialNumber,attr"`
	Version      int             `json:"version" xml:"version"`
	Metadata     cdxMetadata     `json:"metadata" xml:"metadata"`
	Components   []cdxComponent  `json:"components" xml:"components>component"`
	Dependencies []cdxDependency `json:"dependencies" xml:"dependencies>dependency"`
	Vulns        []cdxVuln       `json:"vulnerabilities,omitempty" xml:"vulnerabilities>vulnerability,omitempty"`
}

type cdxMetadata struct {
	Timestamp string        `json:"timestamp" xml:"timestamp"`
	Tools     *cdxTools     `json:"tools,omitempty" xml:"tools,omitempty"`
	Component *cdxComponent `json:"component,omitempty" xml:"component,omitempty"`
}

type cdxTools struct {
	Components []cdxComponent `json:"components" xml:"components>component"`
}

type cdxComponent struct {
	Type     string             `json:"type" xml:"type,attr"`
	Name     string             `json:"name" xml:"name"`
	Version  string             `json:"version,omitempty" xml:"version,omitempty"`
	BOMRef   string             `json:"bom-ref,omitempty" xml:"bom-ref,attr,omitempty"`
	PURL     string             `json:"purl,omitempty" xml:"purl,omitempty"`
	Licenses []cdxLicenseChoice `json:"licenses,omitempty" xml:"licenses>license,omitempty"`
	ExtRefs  []cdxExtRef        `json:"externalReferences,omitempty" xml:"externalReferences>reference,omitempty"`
}

// cdxLicenseChoice holds either a single license or an SPDX expression,
// per the CycloneDX schema (exactly one of the fields is set).
type cdxLicenseChoice struct {
	License    *cdxLicense `json:"license,omitempty" xml:"license,omitempty"`
	Expression string      `json:"expression,omitempty" xml:"expression,omitempty"`
}

type cdxLicense struct {
	ID   string `json:"id,omitempty" xml:"id,omitempty"`
	Name string `json:"name,omitempty" xml:"name,omitempty"`
}

// cdxLicenseFor maps a raw license string to the appropriate CycloneDX
// representation: SPDX id, SPDX expression, or free-text name.
func cdxLicenseFor(license string) cdxLicenseChoice {
	switch {
	case isSPDXID(license):
		return cdxLicenseChoice{License: &cdxLicense{ID: license}}
	case isSPDXExpression(license):
		return cdxLicenseChoice{Expression: license}
	default:
		return cdxLicenseChoice{License: &cdxLicense{Name: license}}
	}
}

type cdxExtRef struct {
	Type string `json:"type" xml:"type,attr"`
	URL  string `json:"url" xml:"url"`
}

type cdxDependency struct {
	Ref       string   `json:"ref" xml:"ref,attr"`
	DependsOn []string `json:"dependsOn" xml:"dependency,omitempty"`
}

type cdxVuln struct {
	BOMRef      string        `json:"bom-ref" xml:"bom-ref,attr"`
	ID          string        `json:"id" xml:"id"`
	Source      *cdxSource    `json:"source,omitempty" xml:"source,omitempty"`
	References  []cdxVulnRef  `json:"references,omitempty" xml:"references>reference,omitempty"`
	Ratings     []cdxRating   `json:"ratings,omitempty" xml:"ratings>rating,omitempty"`
	Description string        `json:"description,omitempty" xml:"description,omitempty"`
	Advisories  []cdxAdvisory `json:"advisories,omitempty" xml:"advisories>advisory,omitempty"`
	Affects     []cdxAffects  `json:"affects" xml:"affects>target"`
}

// cdxVulnRef lists alternative identifiers (e.g. CVE aliases of a GHSA id).
type cdxVulnRef struct {
	ID string `json:"id" xml:"id"`
}

type cdxAdvisory struct {
	URL string `json:"url" xml:"url"`
}

type cdxSource struct {
	Name string `json:"name" xml:"name"`
	URL  string `json:"url" xml:"url"`
}

type cdxRating struct {
	Severity string `json:"severity" xml:"severity"`
}

type cdxAffects struct {
	Ref string `json:"ref" xml:"ref,attr"`
}

// GenerateCycloneDX builds a CycloneDX SBOM from a DAG.
func GenerateCycloneDX(g *dag.DAG, opts Options) ([]byte, error) {
	specVersion := opts.SpecVersion
	if specVersion == "" {
		specVersion = "1.6"
	}

	language := opts.Language
	if language == "" {
		if l, ok := g.Meta()["language"].(string); ok {
			language = l
		}
	}

	root := dag.FindRoot(g)
	rootVersion := ""
	if n, ok := g.Node(root); ok && n.Meta != nil {
		rootVersion, _ = n.Meta["version"].(string)
	}

	bom := cdxBOM{
		BOMFormat:    "CycloneDX",
		SpecVersion:  specVersion,
		SerialNumber: fmt.Sprintf("urn:uuid:%s", pseudoUUID(root, time.Now())),
		Version:      1,
		Metadata: cdxMetadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Component: &cdxComponent{
				Type:    "application",
				Name:    root,
				Version: rootVersion,
				BOMRef:  root,
			},
		},
	}

	if opts.ToolName != "" {
		bom.Metadata.Tools = &cdxTools{
			Components: []cdxComponent{{
				Type:    "application",
				Name:    opts.ToolName,
				Version: opts.ToolVersion,
			}},
		}
	}

	// Components (all non-root, non-synthetic nodes)
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" || n.ID == root {
			continue
		}

		version := ""
		license := ""
		repoURL := ""
		if n.Meta != nil {
			version, _ = n.Meta["version"].(string)
			license, _ = n.Meta["license"].(string)
			repoURL, _ = n.Meta["repo_url"].(string)
		}

		comp := cdxComponent{
			Type:    "library",
			Name:    n.ID,
			Version: version,
			BOMRef:  n.ID,
			PURL:    BuildPURL(language, n.ID, version),
		}

		if license != "" {
			comp.Licenses = []cdxLicenseChoice{cdxLicenseFor(license)}
		}
		if repoURL != "" {
			comp.ExtRefs = []cdxExtRef{{Type: "vcs", URL: repoURL}}
		}

		bom.Components = append(bom.Components, comp)
	}

	// Dependencies
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" {
			continue
		}
		children := g.Children(n.ID)
		var deps []string
		for _, c := range children {
			cn, ok := g.Node(c)
			if !ok || cn.IsSynthetic() {
				continue
			}
			deps = append(deps, c)
		}
		bom.Dependencies = append(bom.Dependencies, cdxDependency{
			Ref:       n.ID,
			DependsOn: deps,
		})
	}

	// Vulnerabilities from the scan report. Nodes annotated only with a
	// severity (no stored report) are intentionally not exported: synthetic
	// IDs would be unusable by downstream tools like Dependency-Track.
	if opts.VulnReport != nil {
		for i, f := range opts.VulnReport.Findings {
			v := cdxVuln{
				BOMRef: fmt.Sprintf("vuln-%d", i+1),
				ID:     f.ID,
				Source: &cdxSource{
					Name: "OSV",
					URL:  "https://osv.dev",
				},
				Ratings:     []cdxRating{{Severity: string(f.Severity)}},
				Description: f.Summary,
				Affects:     []cdxAffects{{Ref: f.Package}},
			}
			for _, alias := range f.Aliases {
				v.References = append(v.References, cdxVulnRef{ID: alias})
			}
			for _, ref := range f.References {
				v.Advisories = append(v.Advisories, cdxAdvisory{URL: ref})
			}
			bom.Vulns = append(bom.Vulns, v)
		}
	}

	switch opts.Encoding {
	case EncodingXML:
		return xml.MarshalIndent(bom, "", "  ")
	default:
		return json.MarshalIndent(bom, "", "  ")
	}
}

// pseudoUUID generates a deterministic-ish UUID for the serial number.
func pseudoUUID(seed string, t time.Time) string {
	h := uint64(0)
	for _, b := range []byte(seed) {
		h = h*31 + uint64(b)
	}
	h ^= uint64(t.UnixNano())
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h&0xffffffff,
		(h>>32)&0xffff,
		((h>>48)&0x0fff)|0x4000,
		((h>>60)&0x3f)|0x80,
		h&0xffffffffffff,
	)
}
