package integrations

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SemanticVersion represents a parsed semantic version.
type SemanticVersion struct {
	Original   string
	Major      int
	Minor      int
	Patch      int
	Prerelease string // e.g., "alpha", "beta.1", "rc.1"
	Build      string // e.g., build metadata after '+'
	Valid      bool
	// Pseudo is true for Go pseudo-versions like
	// v1.2.3-0.20230101120000-abcdef123456, which are synthesized from
	// commits and should sort below any tagged release of the same base.
	Pseudo bool
}

// semverRegex matches semantic versions with optional 'v' prefix.
// Captures: major, minor (optional), patch (optional), prerelease (optional), build (optional).
// Prerelease and build identifiers allow hyphens per the semver spec
// (e.g. "1.0.0-alpha-1", "1.0.0-0.20230101120000-abcdef123456").
var semverRegex = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.-]+))?(?:\+([\w.-]+))?$`)

// goPseudoVersionRegex matches the prerelease portion of Go pseudo-versions:
// a 14-digit UTC timestamp followed by a 12-hex-digit commit prefix, optionally
// preceded by "0." / "<pre>.0." (see https://go.dev/ref/mod#pseudo-versions).
var goPseudoVersionRegex = regexp.MustCompile(`(?:^|\.)\d{14}-[0-9a-f]{12}$`)

// ParseSemver parses a version string into a SemanticVersion.
// Supports formats: "1", "1.2", "1.2.3", "v1.2.3", "1.2.3-beta", "1.2.3+build"
func ParseSemver(version string) SemanticVersion {
	sv := SemanticVersion{Original: version}
	version = strings.TrimSpace(version)

	m := semverRegex.FindStringSubmatch(version)
	if m == nil {
		// Try to handle non-standard versions gracefully
		return sv
	}

	sv.Valid = true
	sv.Major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		sv.Minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		sv.Patch, _ = strconv.Atoi(m[3])
	}
	sv.Prerelease = m[4]
	sv.Build = m[5]
	sv.Pseudo = sv.Prerelease != "" && goPseudoVersionRegex.MatchString(sv.Prerelease)

	return sv
}

// Compare compares two SemanticVersions.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Invalid versions are sorted to the end.
func (a SemanticVersion) Compare(b SemanticVersion) int {
	// Invalid versions sort to the end
	if !a.Valid && !b.Valid {
		return strings.Compare(a.Original, b.Original)
	}
	if !a.Valid {
		return 1
	}
	if !b.Valid {
		return -1
	}

	// Compare major.minor.patch
	if a.Major != b.Major {
		return intCmp(a.Major, b.Major)
	}
	if a.Minor != b.Minor {
		return intCmp(a.Minor, b.Minor)
	}
	if a.Patch != b.Patch {
		return intCmp(a.Patch, b.Patch)
	}

	// Compare prerelease (empty > non-empty, e.g., 1.0.0 > 1.0.0-alpha)
	return comparePrerelease(a.Prerelease, b.Prerelease)
}

func intCmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// comparePrerelease compares prerelease strings according to semver spec.
// A version without prerelease has higher precedence than one with prerelease.
func comparePrerelease(a, b string) int {
	// Empty prerelease (stable release) > non-empty (prerelease)
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1 // a is stable, b is prerelease
	}
	if b == "" {
		return -1 // a is prerelease, b is stable
	}

	// Compare prerelease identifiers (dot-separated)
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		cmp := comparePrereleaseIdentifier(aParts[i], bParts[i])
		if cmp != 0 {
			return cmp
		}
	}

	// Longer prerelease has higher precedence (e.g., alpha.1.2 > alpha.1)
	return intCmp(len(aParts), len(bParts))
}

// comparePrereleaseIdentifier compares individual prerelease identifiers.
// Numeric identifiers are compared as integers; alphanumeric as strings.
// Numeric identifiers have lower precedence than alphanumeric.
func comparePrereleaseIdentifier(a, b string) int {
	aNum, aErr := strconv.Atoi(a)
	bNum, bErr := strconv.Atoi(b)

	aIsNum := aErr == nil
	bIsNum := bErr == nil

	if aIsNum && bIsNum {
		return intCmp(aNum, bNum)
	}
	if aIsNum {
		return -1 // numeric < alphanumeric
	}
	if bIsNum {
		return 1 // alphanumeric > numeric
	}
	return strings.Compare(a, b)
}

// parseAll parses each version string once so sorting doesn't re-parse on
// every comparison (registries commonly list thousands of versions).
func parseAll(versions []string) []SemanticVersion {
	parsed := make([]SemanticVersion, len(versions))
	for i, v := range versions {
		parsed[i] = ParseSemver(v)
	}
	return parsed
}

// SortVersions sorts a slice of version strings in ascending semantic version order.
// Non-semver versions are sorted to the end alphabetically.
func SortVersions(versions []string) {
	parsed := parseAll(versions)
	sort.Sort(&versionSorter{versions: versions, parsed: parsed, less: func(a, b SemanticVersion) bool {
		return a.Compare(b) < 0
	}})
}

// SortVersionsDescending sorts a slice of version strings in descending semantic version order.
// This puts the latest (highest) version first. Non-semver versions are
// sorted to the end alphabetically, mirroring [SortVersions] behaviour.
func SortVersionsDescending(versions []string) {
	parsed := parseAll(versions)
	sort.Sort(&versionSorter{versions: versions, parsed: parsed, less: func(a, b SemanticVersion) bool {
		// Keep non-semver versions at the tail.
		if !a.Valid && !b.Valid {
			return strings.Compare(a.Original, b.Original) > 0
		}
		if !a.Valid {
			return false
		}
		if !b.Valid {
			return true
		}
		return a.Compare(b) > 0
	}})
}

// versionSorter sorts a version string slice and its pre-parsed counterpart
// in lockstep, avoiding repeated parsing during comparisons.
type versionSorter struct {
	versions []string
	parsed   []SemanticVersion
	less     func(a, b SemanticVersion) bool
}

func (s *versionSorter) Len() int           { return len(s.versions) }
func (s *versionSorter) Less(i, j int) bool { return s.less(s.parsed[i], s.parsed[j]) }
func (s *versionSorter) Swap(i, j int) {
	s.versions[i], s.versions[j] = s.versions[j], s.versions[i]
	s.parsed[i], s.parsed[j] = s.parsed[j], s.parsed[i]
}

// LatestVersion returns the highest semantic version from a slice.
// Returns empty string if the slice is empty.
func LatestVersion(versions []string) string {
	if len(versions) == 0 {
		return ""
	}

	latest := versions[0]
	latestParsed := ParseSemver(latest)

	for _, v := range versions[1:] {
		parsed := ParseSemver(v)
		if parsed.Compare(latestParsed) > 0 {
			latest = v
			latestParsed = parsed
		}
	}

	return latest
}
