package deps

import (
	"maps"
	"regexp"
	"strings"

	"github.com/stacktower-io/stacktower/pkg/core/dag"
)

// FilterPrereleaseNodes removes prerelease versions from the graph unless
// includePrerelease is true.
func FilterPrereleaseNodes(g *dag.DAG, includePrerelease bool) *dag.DAG {
	if includePrerelease {
		return g
	}

	// Keep roots and walk reachability from roots while skipping prerelease nodes.
	visited := make(map[string]bool, g.NodeCount())
	queue := make([]string, 0, g.NodeCount())
	for _, n := range g.Nodes() {
		if len(g.Parents(n.ID)) == 0 {
			visited[n.ID] = true
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range g.Children(cur) {
			if visited[child] {
				continue
			}
			if childNode, ok := g.Node(child); ok && isPrereleaseNode(childNode) {
				continue
			}
			visited[child] = true
			queue = append(queue, child)
		}
	}

	filtered := dag.New(nil)
	maps.Copy(filtered.Meta(), g.Meta())

	for _, n := range g.Nodes() {
		if !visited[n.ID] {
			continue
		}
		_ = filtered.AddNode(dag.Node{
			ID:       n.ID,
			Row:      n.Row,
			Meta:     maps.Clone(n.Meta),
			Kind:     n.Kind,
			MasterID: n.MasterID,
		})
	}
	for _, e := range g.Edges() {
		if !visited[e.From] || !visited[e.To] {
			continue
		}
		_ = filtered.AddEdge(dag.Edge{
			From: e.From,
			To:   e.To,
			Meta: maps.Clone(e.Meta),
		})
	}
	return filtered
}

func isPrereleaseNode(n *dag.Node) bool {
	if n == nil {
		return false
	}
	rawVersion, ok := n.Meta["version"]
	if !ok {
		return false
	}
	version, ok := rawVersion.(string)
	if !ok {
		return false
	}
	return IsPrereleaseVersion(version)
}

// goPseudoVersionSuffixRE matches Go pseudo-version suffixes: a 14-digit UTC
// timestamp followed by a 12-hex-digit commit prefix, regardless of the base
// version (see https://go.dev/ref/mod#pseudo-versions). Examples:
// "v0.0.0-20260218203240-3dfff04db8fa", "v1.2.4-0.20230101120000-abcdef123456".
var goPseudoVersionSuffixRE = regexp.MustCompile(`(?:^|[.-])\d{14}-[0-9a-f]{12}$`)

// pep440PrereleaseRE matches whole versions carrying PEP 440 style pre/dev
// markers attached without a hyphen separator, e.g. "1.0.0a1", "2.13.0b12",
// "1.0rc2", "1.0.0.dev3". Post releases ("1.0.0.post1") are stable and do
// not match.
var pep440PrereleaseRE = regexp.MustCompile(`^v?\d+(?:\.\d+)*[._-]?(?:a|b|c|rc|alpha|beta|pre|preview|dev)\d*$`)

// prereleaseMarkers are identifiers that mark a version as prerelease when
// they appear as a whole identifier (optionally followed by digits) in the
// prerelease segment, e.g. "1.0.0-alpha.1", "2.0.0-RC2", "1.0-SNAPSHOT".
var prereleaseMarkers = map[string]bool{
	"alpha":       true,
	"beta":        true,
	"rc":          true,
	"cr":          true, // JBoss-style candidate release
	"dev":         true,
	"development": true,
	"snapshot":    true,
	"preview":     true,
	"pre":         true,
	"prerelease":  true,
	"canary":      true,
	"nightly":     true,
	"next":        true,
	"milestone":   true, // Maven milestone releases
}

// IsPrereleaseVersion checks if a version string represents a prerelease.
// It detects common prerelease markers like alpha, beta, rc, dev, canary,
// nightly, next, etc., as well as PEP 440 style markers (e.g., 1.0.0a1) and
// Maven milestones (e.g., 7.0.0-M6).
//
// Markers are only matched as whole identifiers within the prerelease segment
// (after the first '-') so that commit hashes or arbitrary words containing
// marker substrings (e.g. "1.0.0-MUSL", hex hashes containing "rc") are not
// misclassified. Go pseudo-versions are never treated as prerelease channels.
func IsPrereleaseVersion(version string) bool {
	v := strings.ToLower(strings.TrimSpace(version))
	if v == "" {
		return false
	}

	// Strip semver build metadata; it has no effect on precedence.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}

	// Go pseudo versions are synthesized from commits and should not be
	// treated as prerelease channels, regardless of their base version.
	if goPseudoVersionSuffixRE.MatchString(v) {
		return false
	}

	// PEP 440 abbreviated markers without a hyphen (e.g., "2.13.0b1", "1.0.0a1").
	if pep440PrereleaseRE.MatchString(v) {
		return true
	}

	// Semver-style markers: inspect identifiers in the prerelease segment
	// after the first '-' (e.g., "1.0.0-alpha.1", "2.0.0-beta-2", "7.0.0-M6").
	dash := strings.IndexByte(v, '-')
	if dash < 0 || dash == len(v)-1 {
		return false
	}
	identifiers := strings.FieldsFunc(v[dash+1:], func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	for _, ident := range identifiers {
		marker := strings.TrimRight(ident, "0123456789")
		if marker == "" {
			continue
		}
		if prereleaseMarkers[marker] {
			return true
		}
		// Maven milestone pattern: M followed by digits (e.g., "7.0.0-M6").
		if marker == "m" && len(ident) > 1 {
			return true
		}
	}
	return false
}
