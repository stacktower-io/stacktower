package javascript

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// SemverMatcher implements constraint matching for npm's semver specifiers.
// It supports common operators: =, !=, <, <=, >, >=, ^, ~, x-range, hyphen range
type SemverMatcher struct{}

// Ensure SemverMatcher implements ConstraintParser
var _ deps.ConstraintParser = SemverMatcher{}

// semverVersion holds a parsed semantic version
type semverVersion struct {
	original   string
	major      int
	minor      int
	patch      int
	prerelease string
	valid      bool
}

var (
	// Matches semver: major.minor.patch with optional prerelease
	semverRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.]+))?(?:\+[\w.]+)?$`)

	// Matches constraint operators
	operatorRE = regexp.MustCompile(`^\s*(>=?|<=?|=|!=|\^|~)?\s*(.+)$`)
)

// parseVersion parses a semver version string.
func parseSemver(v string) semverVersion {
	sv := semverVersion{original: v}
	v = strings.TrimSpace(v)

	m := semverRE.FindStringSubmatch(v)
	if m == nil {
		return sv
	}

	sv.valid = true
	sv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		sv.minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		sv.patch, _ = strconv.Atoi(m[3])
	}
	sv.prerelease = m[4]

	return sv
}

// ParseVersion converts a semver string to a PubGrub SemanticVersion.
func (SemverMatcher) ParseVersion(version string) pubgrub.Version {
	sv := parseSemver(version)
	if !sv.valid {
		return nil
	}
	// Use SemanticVersion for proper ordering
	semVer, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", sv.major, sv.minor, sv.patch))
	if err != nil {
		return pubgrub.SimpleVersion(version)
	}
	return semVer
}

// ParseConstraint converts an npm semver constraint to a PubGrub Condition.
// Supports: ^, ~, >=, <=, >, <, =, !=, x-ranges, hyphen ranges, and || (or)
func (SemverMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	constraint = strings.TrimSpace(constraint)

	// Handle "||" (or) by finding the best match from any alternative
	// For PubGrub, we use union of version sets
	if strings.Contains(constraint, "||") {
		parts := strings.Split(constraint, "||")
		var ranges []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if r := constraintToRange(part); r != "" {
				ranges = append(ranges, r)
			}
		}
		if len(ranges) == 0 {
			return nil
		}
		// Join with || for union in PubGrub
		rangeStr := strings.Join(ranges, " || ")
		versionSet, err := pubgrub.ParseVersionRange(rangeStr)
		if err != nil {
			return nil
		}
		return pubgrub.NewVersionSetCondition(versionSet)
	}

	// Handle single constraint (may contain space-separated AND)
	rangeStr := constraintToRange(constraint)
	if rangeStr == "" {
		return nil
	}

	versionSet, err := pubgrub.ParseVersionRange(rangeStr)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(versionSet)
}

// normalizeSpacedOperators fixes npm constraints where operators have spaces
// before the version number, like ">= 2.1.2 < 3" -> ">=2.1.2 <3"
var spacedOperatorRE = regexp.MustCompile(`(>=?|<=?|=|!=|\^|~)\s+(\d)`)

func normalizeConstraint(constraint string) string {
	return spacedOperatorRE.ReplaceAllString(constraint, "$1$2")
}

// constraintToRange converts a single npm constraint to PubGrub range syntax.
func constraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" || constraint == "latest" {
		return "*"
	}

	// Normalize spaced operators: ">= 2.1.2" -> ">=2.1.2"
	constraint = normalizeConstraint(constraint)

	// Handle hyphen range: "1.0.0 - 2.0.0"
	if strings.Contains(constraint, " - ") {
		parts := strings.SplitN(constraint, " - ", 2)
		if len(parts) == 2 {
			from := parseSemver(strings.TrimSpace(parts[0]))
			to := parseSemver(strings.TrimSpace(parts[1]))
			if from.valid && to.valid {
				return fmt.Sprintf(">=%d.%d.%d, <=%d.%d.%d",
					from.major, from.minor, from.patch,
					to.major, to.minor, to.patch)
			}
		}
	}

	// Handle space-separated (AND) constraints: ">=1.0.0 <2.0.0"
	if strings.Contains(constraint, " ") && !strings.Contains(constraint, " - ") {
		parts := strings.Fields(constraint)
		var ranges []string
		for _, part := range parts {
			if r := singleConstraintToRange(part); r != "" {
				ranges = append(ranges, r)
			}
		}
		return strings.Join(ranges, ", ")
	}

	return singleConstraintToRange(constraint)
}

// singleConstraintToRange converts a single constraint (no spaces) to range syntax.
func singleConstraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}

	// Handle x-range: 1.x, 1.2.x, *, 1.*
	// Check individual dot-separated parts for wildcards rather than scanning
	// the whole string, which would false-positive on prerelease tags like "1.0.0-next".
	parts := strings.Split(constraint, ".")
	hasWildcardPart := false
	for _, p := range parts {
		if isWildcard(p) {
			hasWildcardPart = true
			break
		}
	}
	if hasWildcardPart {
		// Replace x/X/* with 0 and expand to range
		normalized := strings.NewReplacer("x", "0", "X", "0", "*", "0").Replace(constraint)
		sv := parseSemver(normalized)
		if sv.valid {
			// Determine which parts were wildcards
			if isWildcard(parts[0]) {
				return "*" // Any version
			}
			if len(parts) == 1 || (len(parts) >= 2 && isWildcard(parts[1])) {
				// 1.x or 1.* -> >=1.0.0, <2.0.0
				return fmt.Sprintf(">=%d.0.0, <%d.0.0", sv.major, sv.major+1)
			}
			if len(parts) >= 3 && isWildcard(parts[2]) {
				// 1.2.x or 1.2.* -> >=1.2.0, <1.3.0
				return fmt.Sprintf(">=%d.%d.0, <%d.%d.0", sv.major, sv.minor, sv.major, sv.minor+1)
			}
		}
	}

	m := operatorRE.FindStringSubmatch(constraint)
	if m == nil {
		return ""
	}

	op := m[1]
	versionStr := m[2]
	sv := parseSemver(versionStr)
	if !sv.valid {
		return ""
	}

	switch op {
	case "^":
		// Caret: ^1.2.3 -> >=1.2.3, <2.0.0 (allows minor/patch changes)
		// ^0.2.3 -> >=0.2.3, <0.3.0 (when major is 0, minor is significant)
		// ^0.0.3 -> >=0.0.3, <0.0.4 (when major.minor is 0.0, only patch changes)
		if sv.major == 0 {
			if sv.minor == 0 {
				// ^0.0.x -> only exact patch
				return fmt.Sprintf(">=%d.%d.%d, <%d.%d.%d",
					sv.major, sv.minor, sv.patch,
					sv.major, sv.minor, sv.patch+1)
			}
			// ^0.x.y -> minor is significant
			return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0",
				sv.major, sv.minor, sv.patch,
				sv.major, sv.minor+1)
		}
		return fmt.Sprintf(">=%d.%d.%d, <%d.0.0",
			sv.major, sv.minor, sv.patch,
			sv.major+1)

	case "~":
		// Tilde: ~1.2.3 -> >=1.2.3, <1.3.0 (allows patch changes)
		return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0",
			sv.major, sv.minor, sv.patch,
			sv.major, sv.minor+1)

	case ">=":
		return fmt.Sprintf(">=%d.%d.%d", sv.major, sv.minor, sv.patch)

	case ">":
		return fmt.Sprintf(">%d.%d.%d", sv.major, sv.minor, sv.patch)

	case "<=":
		return fmt.Sprintf("<=%d.%d.%d", sv.major, sv.minor, sv.patch)

	case "<":
		return fmt.Sprintf("<%d.%d.%d", sv.major, sv.minor, sv.patch)

	case "=", "":
		// Exact match or no operator (bare version)
		return fmt.Sprintf("==%d.%d.%d", sv.major, sv.minor, sv.patch)

	case "!=":
		return fmt.Sprintf("!=%d.%d.%d", sv.major, sv.minor, sv.patch)

	default:
		return ""
	}
}

func isWildcard(s string) bool {
	return s == "x" || s == "X" || s == "*"
}
