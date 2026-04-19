package sbom

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// SPDX 2.3 JSON types

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	DocumentName      string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	SPDXID           string             `json:"SPDXID"`
	Name             string             `json:"name"`
	VersionInfo      string             `json:"versionInfo,omitempty"`
	DownloadLocation string             `json:"downloadLocation"`
	FilesAnalyzed    bool               `json:"filesAnalyzed"`
	LicenseConcluded string             `json:"licenseConcluded,omitempty"`
	LicenseDeclared  string             `json:"licenseDeclared,omitempty"`
	CopyrightText    string             `json:"copyrightText"`
	ExternalRefs     []spdxExternalRef  `json:"externalRefs,omitempty"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxRelationship struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

// GenerateSPDX builds an SPDX 2.3 SBOM from a DAG.
func GenerateSPDX(g *dag.DAG, opts Options) ([]byte, error) {
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

	toolCreator := "Tool: stacktower"
	if opts.ToolVersion != "" {
		toolCreator = fmt.Sprintf("Tool: stacktower-%s", opts.ToolVersion)
	}

	doc := spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		DocumentName:      root,
		DocumentNamespace: fmt.Sprintf("https://stacktower.io/spdx/%s/%s", root, time.Now().UTC().Format("20060102")),
		CreationInfo: spdxCreationInfo{
			Created:  time.Now().UTC().Format(time.RFC3339),
			Creators: []string{toolCreator},
		},
	}

	// Root package
	doc.Packages = append(doc.Packages, spdxPackage{
		SPDXID:           spdxID(root),
		Name:             root,
		VersionInfo:      rootVersion,
		DownloadLocation: "NOASSERTION",
		CopyrightText:    "NOASSERTION",
	})
	doc.Relationships = append(doc.Relationships, spdxRelationship{
		Element: "SPDXRef-DOCUMENT",
		Type:    "DESCRIBES",
		Related: spdxID(root),
	})

	// Dependency packages
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" || n.ID == root {
			continue
		}

		version := ""
		license := ""
		if n.Meta != nil {
			version, _ = n.Meta["version"].(string)
			license, _ = n.Meta["license"].(string)
		}

		pkg := spdxPackage{
			SPDXID:           spdxID(n.ID),
			Name:             n.ID,
			VersionInfo:      version,
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    false,
			CopyrightText:    "NOASSERTION",
		}

		if license != "" {
			pkg.LicenseConcluded = license
			pkg.LicenseDeclared = license
		} else {
			pkg.LicenseConcluded = "NOASSERTION"
			pkg.LicenseDeclared = "NOASSERTION"
		}

		purl := BuildPURL(language, n.ID, version)
		if purl != "" {
			pkg.ExternalRefs = []spdxExternalRef{{
				ReferenceCategory: "PACKAGE-MANAGER",
				ReferenceType:     "purl",
				ReferenceLocator:  purl,
			}}
		}

		doc.Packages = append(doc.Packages, pkg)
	}

	// Relationships from edges
	for _, n := range g.Nodes() {
		if n.IsSynthetic() || n.ID == "__project__" {
			continue
		}
		for _, child := range g.Children(n.ID) {
			cn, ok := g.Node(child)
			if !ok || cn.IsSynthetic() {
				continue
			}
			doc.Relationships = append(doc.Relationships, spdxRelationship{
				Element: spdxID(n.ID),
				Type:    "DEPENDS_ON",
				Related: spdxID(child),
			})
		}
	}

	return json.MarshalIndent(doc, "", "  ")
}

// spdxID converts a package name to a valid SPDX identifier.
func spdxID(name string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return '-'
	}, name)
	return "SPDXRef-" + safe
}
