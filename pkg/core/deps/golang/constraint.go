package golang

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// GoModMatcher implements constraint matching for Go module versions.
// Go uses semantic versioning with some specific rules:
// - Versions are prefixed with 'v': v1.2.3
// - Major version 2+ requires path suffix: module/v2
// - Pre-release and build metadata follow semver spec
type GoModMatcher struct{}

// Ensure GoModMatcher implements ConstraintParser and VersionHinter
var _ deps.ConstraintParser = GoModMatcher{}
var _ deps.VersionHinter = GoModMatcher{}

// goVersion holds a parsed Go module version
type goVersion struct {
	original   string
	major      int
	minor      int
	patch      int
	prerelease string
	valid      bool
}

var (
	// Matches Go module version: v1.2.3, v1.2.3-beta.1, v1.2.3+incompatible
	// Also handles pseudo-versions like v1.0.0-20201130134442-10cb98267c6c
	goVersionRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.\-]+))?(?:\+[\w.]+)?$`)
)

// parseGoVersion parses a Go module version string.
func parseGoVersion(v string) goVersion {
	gv := goVersion{original: v}
	v = strings.TrimSpace(v)

	// Remove leading 'v' for parsing
	v = strings.TrimPrefix(v, "v")

	m := goVersionRE.FindStringSubmatch("v" + v)
	if m == nil {
		return gv
	}

	gv.valid = true
	gv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		gv.minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		gv.patch, _ = strconv.Atoi(m[3])
	}
	gv.prerelease = m[4]

	return gv
}

// ParseVersion converts a Go module version string to a PubGrub Version.
// All valid versions — including pseudo-versions like v0.0.0-20201130134442-...
// — are returned as *SemanticVersion so they participate in range comparison
// (required for MVS >= constraints). Pseudo-version prerelease strings sort
// below real releases in semver ordering, which is exactly what MVS needs.
func (GoModMatcher) ParseVersion(version string) pubgrub.Version {
	gv := parseGoVersion(version)
	if !gv.valid {
		return nil
	}
	if gv.prerelease != "" {
		return pubgrub.NewSemanticVersionWithPrerelease(gv.major, gv.minor, gv.patch, gv.prerelease)
	}
	semVer, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", gv.major, gv.minor, gv.patch))
	if err != nil {
		return pubgrub.SimpleVersion(goCanonical(gv))
	}
	return semVer
}

// ParseConstraint converts a Go module version constraint to a PubGrub Condition.
// Go uses Minimum Version Selection (MVS): every require directive — whether a
// clean release like v1.2.3 or a pseudo-version like v0.0.0-20210809222454-... —
// means "at least this version". We emit a >= lower-bound for all SemanticVersion
// values so PubGrub can pick a higher compatible version when two modules need
// different minimums. Only non-semver fallbacks (SimpleVersion) use exact match.
//
// The "=" prefix is stripped if present (added by goproxy.parseRequireLine for
// internal representation) but doesn't change semantics - Go always uses MVS.
func (GoModMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	// Strip "=" prefix if present (internal representation, doesn't change MVS semantics)
	constraint = strings.TrimPrefix(constraint, "=")

	v := GoModMatcher{}.ParseVersion(constraint)
	if v == nil {
		return nil
	}

	// Go MVS: all constraints are lower bounds (>= version)
	if _, ok := v.(*pubgrub.SemanticVersion); ok {
		return pubgrub.NewVersionSetCondition(pubgrub.NewLowerBoundVersionSet(v, true))
	}
	// Non-semver fallback (rare) uses exact match
	return pubgrub.EqualsCondition{Version: v}
}

// HintedVersion implements deps.VersionHinter. Go modules use "=version"
// constraints for pinned versions in go.mod files. These exact versions
// (especially pseudo-versions like v0.0.0-20231006140011-abc123) may not
// appear in the Go proxy's @v/list endpoint, so we hint them to ensure
// PubGrub can find them during version selection.
func (GoModMatcher) HintedVersion(constraint string) string {
	if !strings.HasPrefix(constraint, "=") {
		return ""
	}
	return strings.TrimPrefix(constraint, "=")
}

// goCanonical returns a consistent string representation of a Go version
// for use as a SimpleVersion key. Includes prerelease for pseudo-versions.
func goCanonical(gv goVersion) string {
	base := fmt.Sprintf("%d.%d.%d", gv.major, gv.minor, gv.patch)
	if gv.prerelease != "" {
		return base + "-" + gv.prerelease
	}
	return base
}
