package sbom

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
	"github.com/stacktower-io/stacktower/pkg/security"
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

type cdxLicenseChoice struct {
	License cdxLicense `json:"license" xml:"license"`
}

type cdxLicense struct {
	ID string `json:"id,omitempty" xml:"id,omitempty"`
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
	BOMRef  string       `json:"bom-ref" xml:"bom-ref,attr"`
	ID      string       `json:"id" xml:"id"`
	Source  *cdxSource   `json:"source,omitempty" xml:"source,omitempty"`
	Ratings []cdxRating  `json:"ratings,omitempty" xml:"ratings>rating,omitempty"`
	Affects []cdxAffects `json:"affects" xml:"affects>target"`
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
			comp.Licenses = []cdxLicenseChoice{{License: cdxLicense{ID: license}}}
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

	// Vulnerabilities from report
	if opts.VulnReport != nil {
		for i, f := range opts.VulnReport.Findings {
			bom.Vulns = append(bom.Vulns, cdxVuln{
				BOMRef: fmt.Sprintf("vuln-%d", i+1),
				ID:     f.ID,
				Source: &cdxSource{
					Name: "OSV",
					URL:  "https://osv.dev",
				},
				Ratings: []cdxRating{{Severity: string(f.Severity)}},
				Affects: []cdxAffects{{Ref: f.Package}},
			})
		}
	} else {
		// Fall back to node-level vuln metadata
		for _, n := range g.Nodes() {
			if n.Meta == nil {
				continue
			}
			sev, ok := n.Meta[security.MetaVulnSeverity].(string)
			if !ok || sev == "" {
				continue
			}
			bom.Vulns = append(bom.Vulns, cdxVuln{
				BOMRef:  fmt.Sprintf("vuln-%s", n.ID),
				ID:      fmt.Sprintf("vuln-%s", n.ID),
				Ratings: []cdxRating{{Severity: sev}},
				Affects: []cdxAffects{{Ref: n.ID}},
			})
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
