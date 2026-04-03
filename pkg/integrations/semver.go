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
}

// semverRegex matches semantic versions with optional 'v' prefix.
// Captures: major, minor (optional), patch (optional), prerelease (optional), build (optional)
var semverRegex = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.]+))?(?:\+([\w.]+))?$`)

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
	aNum, aIsNum := strconv.Atoi(a)
	bNum, bIsNum := strconv.Atoi(b)

	if aIsNum == nil && bIsNum == nil {
		return intCmp(aNum, bNum)
	}
	if aIsNum == nil {
		return -1 // numeric < alphanumeric
	}
	if bIsNum == nil {
		return 1 // alphanumeric > numeric
	}
	return strings.Compare(a, b)
}

// SortVersions sorts a slice of version strings in ascending semantic version order.
// Non-semver versions are sorted to the end alphabetically.
func SortVersions(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		vi := ParseSemver(versions[i])
		vj := ParseSemver(versions[j])
		return vi.Compare(vj) < 0
	})
}

// SortVersionsDescending sorts a slice of version strings in descending semantic version order.
// This puts the latest (highest) version first. Non-semver versions are
// sorted to the end alphabetically, mirroring [SortVersions] behaviour.
func SortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		vi := ParseSemver(versions[i])
		vj := ParseSemver(versions[j])

		// Keep non-semver versions at the tail.
		if !vi.Valid && !vj.Valid {
			return strings.Compare(vi.Original, vj.Original) > 0
		}
		if !vi.Valid {
			return false
		}
		if !vj.Valid {
			return true
		}
		return vi.Compare(vj) > 0
	})
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
