package java

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// MavenMatcher implements constraint matching for Maven version ranges.
// Maven version syntax:
// - Exact: 1.0
// - Range: [1.0,2.0), (1.0,2.0], [1.0,), (,2.0]
// - Multiple ranges: [1.0,2.0),[3.0,4.0)
type MavenMatcher struct{}

// Ensure MavenMatcher implements ConstraintParser
var _ deps.ConstraintParser = MavenMatcher{}

// mavenVersion holds a parsed Maven version
type mavenVersion struct {
	original  string
	major     int
	minor     int
	patch     int
	qualifier string
	valid     bool
}

var (
	// Matches Maven version: 1, 1.2, 1.2.3, 1.2.3-SNAPSHOT, 1.2.3.Final
	mavenVersionRE = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[.-](.+))?$`)

	// Matches Maven range syntax: [1.0,2.0), (1.0,], etc.
	mavenRangeRE = regexp.MustCompile(`^([\[\(])([^,]*),([^)\]]*)([\]\)])$`)
)

// parseMavenVersion parses a Maven version string.
func parseMavenVersion(v string) mavenVersion {
	mv := mavenVersion{original: v}
	v = strings.TrimSpace(v)
	if v == "" {
		return mv
	}

	m := mavenVersionRE.FindStringSubmatch(v)
	if m == nil {
		return mv
	}

	mv.valid = true
	mv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		mv.minor, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		mv.patch, _ = strconv.Atoi(m[3])
	}
	mv.qualifier = m[4]

	return mv
}

// ParseVersion converts a Maven version string to a PubGrub Version.
// We use SimpleVersion to preserve the full version string including qualifiers
// like -jre, -android, -SNAPSHOT, etc. This ensures cache key consistency.
func (MavenMatcher) ParseVersion(version string) pubgrub.Version {
	mv := parseMavenVersion(version)
	if !mv.valid {
		return nil
	}
	// Use SimpleVersion to preserve the original version string.
	// This is critical for Maven because versions like "33.4.8-jre" must stay
	// intact - stripping the qualifier causes cache misses and fetch failures.
	return pubgrub.SimpleVersion(version)
}

// ParseConstraint converts a Maven version range to a PubGrub Condition.
func (m MavenMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	constraint = strings.TrimSpace(constraint)

	// Handle wildcard
	if constraint == "*" {
		vs, _ := pubgrub.ParseVersionRange("*")
		return pubgrub.NewVersionSetCondition(vs)
	}

	// Handle multiple ranges separated by comma (outside brackets)
	// e.g., "[1.0,2.0),[3.0,4.0)" - we need to find the union
	ranges := splitMavenRanges(constraint)
	if len(ranges) > 1 {
		var rangeStrs []string
		for _, r := range ranges {
			if rs := mavenSingleRangeToRange(r); rs != "" {
				rangeStrs = append(rangeStrs, rs)
			}
		}
		if len(rangeStrs) == 0 {
			return nil
		}
		// Join with || for union
		rangeStr := strings.Join(rangeStrs, " || ")
		versionSet, err := pubgrub.ParseVersionRange(rangeStr)
		if err != nil {
			return nil
		}
		return pubgrub.NewVersionSetCondition(versionSet)
	}

	// Check for exact version match with brackets: [1.0]
	if strings.HasPrefix(constraint, "[") && strings.HasSuffix(constraint, "]") && !strings.Contains(constraint, ",") {
		inner := constraint[1 : len(constraint)-1]
		mv := parseMavenVersion(inner)
		if mv.valid {
			// Use EqualsCondition directly with SimpleVersion to preserve the original version string
			return pubgrub.EqualsCondition{Version: pubgrub.SimpleVersion(inner)}
		}
	}

	// Plain version - treat as exact requirement using EqualsCondition
	// This preserves the full version string including qualifiers like -jre, -android, etc.
	mv := parseMavenVersion(constraint)
	if mv.valid {
		return pubgrub.EqualsCondition{Version: pubgrub.SimpleVersion(constraint)}
	}

	// Try as a range expression
	rangeStr := mavenSingleRangeToRange(constraint)
	if rangeStr == "" {
		return nil
	}

	versionSet, err := pubgrub.ParseVersionRange(rangeStr)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(versionSet)
}

// splitMavenRanges splits multiple Maven ranges.
// "[1.0,2.0),[3.0,4.0)" -> ["[1.0,2.0)", "[3.0,4.0)"]
func splitMavenRanges(s string) []string {
	var result []string
	var current strings.Builder
	depth := 0

	for _, c := range s {
		switch c {
		case '[', '(':
			depth++
			current.WriteRune(c)
		case ']', ')':
			depth--
			current.WriteRune(c)
			if depth == 0 {
				r := strings.TrimSpace(current.String())
				if r != "" {
					result = append(result, r)
				}
				current.Reset()
			}
		case ',':
			if depth == 0 {
				// Separator between ranges
				continue
			}
			current.WriteRune(c)
		default:
			current.WriteRune(c)
		}
	}

	// If no ranges found, treat as single version
	if len(result) == 0 && current.Len() > 0 {
		result = append(result, strings.TrimSpace(current.String()))
	}

	return result
}

// mavenSingleRangeToRange converts a single Maven range/version to PubGrub syntax.
func mavenSingleRangeToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}

	// Check if it's a range expression
	m := mavenRangeRE.FindStringSubmatch(constraint)
	if m != nil {
		leftBracket := m[1]  // [ or (
		leftVer := m[2]      // lower bound
		rightVer := m[3]     // upper bound
		rightBracket := m[4] // ] or )

		var conditions []string

		// Handle lower bound
		if leftVer != "" {
			lv := parseMavenVersion(leftVer)
			if lv.valid {
				if leftBracket == "[" {
					conditions = append(conditions, fmt.Sprintf(">=%d.%d.%d", lv.major, lv.minor, lv.patch))
				} else {
					conditions = append(conditions, fmt.Sprintf(">%d.%d.%d", lv.major, lv.minor, lv.patch))
				}
			}
		}

		// Handle upper bound
		if rightVer != "" {
			rv := parseMavenVersion(rightVer)
			if rv.valid {
				if rightBracket == "]" {
					conditions = append(conditions, fmt.Sprintf("<=%d.%d.%d", rv.major, rv.minor, rv.patch))
				} else {
					conditions = append(conditions, fmt.Sprintf("<%d.%d.%d", rv.major, rv.minor, rv.patch))
				}
			}
		}

		if len(conditions) == 0 {
			return "*" // Unbounded range
		}
		return strings.Join(conditions, ", ")
	}

	// Note: Exact matches ([1.0] and plain versions) are handled directly
	// in ParseConstraint using EqualsCondition to preserve version strings.
	// This function should only be called for actual range expressions.
	return ""
}
