package ruby

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// GemMatcher implements constraint matching for RubyGems version requirements.
// RubyGems requirements syntax:
// - Exact: = 1.0.0
// - Not equal: != 1.0.0
// - Greater/Less: >, >=, <, <=
// - Approximate: ~> 1.2 (pessimistic, allows 1.2.x but not 1.3)
// - Multiple requirements: comma-separated (AND)
type GemMatcher struct{}

// Ensure GemMatcher implements ConstraintParser
var _ deps.ConstraintParser = GemMatcher{}

// gemVersion holds a parsed RubyGems version
type gemVersion struct {
	original string
	segments []int // Version segments: [1, 2, 3] for "1.2.3"
	valid    bool
}

var (
	// Matches RubyGems version: 1, 1.2, 1.2.3, 1.2.3.4, etc.
	gemVersionRE = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:[-.]?([a-zA-Z][\w.]*))?$`)

	// Matches constraint operators
	gemOperatorRE = regexp.MustCompile(`^\s*(>=?|<=?|!=|=|~>)\s*(.+)$`)
)

// parseGemVersion parses a RubyGems version string.
func parseGemVersion(v string) gemVersion {
	gv := gemVersion{original: v}
	v = strings.TrimSpace(v)

	m := gemVersionRE.FindStringSubmatch(v)
	if m == nil {
		return gv
	}

	// Parse version segments
	parts := strings.Split(m[1], ".")
	gv.segments = make([]int, len(parts))
	for i, p := range parts {
		gv.segments[i], _ = strconv.Atoi(p)
	}

	gv.valid = len(gv.segments) > 0
	return gv
}

// toSemver converts gem segments to semver format (major.minor.patch).
func (gv gemVersion) toSemver() (int, int, int) {
	major, minor, patch := 0, 0, 0
	if len(gv.segments) > 0 {
		major = gv.segments[0]
	}
	if len(gv.segments) > 1 {
		minor = gv.segments[1]
	}
	if len(gv.segments) > 2 {
		patch = gv.segments[2]
	}
	return major, minor, patch
}

// ParseVersion converts a RubyGems version string to a PubGrub SemanticVersion.
func (GemMatcher) ParseVersion(version string) pubgrub.Version {
	gv := parseGemVersion(version)
	if !gv.valid {
		return nil
	}
	major, minor, patch := gv.toSemver()
	semVer, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", major, minor, patch))
	if err != nil {
		return pubgrub.SimpleVersion(version)
	}
	return semVer
}

// ParseConstraint converts a RubyGems requirement to a PubGrub Condition.
func (GemMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	constraint = strings.TrimSpace(constraint)

	// Handle wildcard
	if constraint == ">= 0" || constraint == "*" {
		vs, _ := pubgrub.ParseVersionRange("*")
		return pubgrub.NewVersionSetCondition(vs)
	}

	// Handle comma-separated (AND) constraints
	if strings.Contains(constraint, ",") {
		parts := strings.Split(constraint, ",")
		var ranges []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if r := gemSingleConstraintToRange(part); r != "" {
				ranges = append(ranges, r)
			}
		}
		if len(ranges) == 0 {
			return nil
		}
		rangeStr := strings.Join(ranges, ", ")
		versionSet, err := pubgrub.ParseVersionRange(rangeStr)
		if err != nil {
			return nil
		}
		return pubgrub.NewVersionSetCondition(versionSet)
	}

	// Single constraint
	rangeStr := gemSingleConstraintToRange(constraint)
	if rangeStr == "" {
		return nil
	}

	versionSet, err := pubgrub.ParseVersionRange(rangeStr)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(versionSet)
}

// gemSingleConstraintToRange converts a single RubyGems constraint to PubGrub syntax.
func gemSingleConstraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}

	m := gemOperatorRE.FindStringSubmatch(constraint)
	if m == nil {
		// No operator - treat as exact version
		gv := parseGemVersion(constraint)
		if gv.valid {
			major, minor, patch := gv.toSemver()
			return fmt.Sprintf("==%d.%d.%d", major, minor, patch)
		}
		return ""
	}

	op := m[1]
	versionStr := m[2]
	gv := parseGemVersion(versionStr)
	if !gv.valid {
		return ""
	}

	major, minor, patch := gv.toSemver()

	switch op {
	case "~>":
		// Pessimistic version constraint (approximate)
		// ~> 1.2.3 means >= 1.2.3, < 1.3.0
		// ~> 1.2 means >= 1.2.0, < 2.0.0
		// ~> 1 means >= 1.0.0, < 2.0.0
		numSegments := len(gv.segments)
		if numSegments >= 3 {
			// ~> 1.2.3 -> >=1.2.3, <1.3.0
			return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0", major, minor, patch, major, minor+1)
		} else if numSegments == 2 {
			// ~> 1.2 -> >=1.2.0, <2.0.0
			return fmt.Sprintf(">=%d.%d.0, <%d.0.0", major, minor, major+1)
		} else {
			// ~> 1 -> >=1.0.0, <2.0.0
			return fmt.Sprintf(">=%d.0.0, <%d.0.0", major, major+1)
		}

	case ">=":
		return fmt.Sprintf(">=%d.%d.%d", major, minor, patch)

	case ">":
		return fmt.Sprintf(">%d.%d.%d", major, minor, patch)

	case "<=":
		return fmt.Sprintf("<=%d.%d.%d", major, minor, patch)

	case "<":
		return fmt.Sprintf("<%d.%d.%d", major, minor, patch)

	case "=":
		return fmt.Sprintf("==%d.%d.%d", major, minor, patch)

	case "!=":
		return fmt.Sprintf("!=%d.%d.%d", major, minor, patch)

	default:
		return ""
	}
}
