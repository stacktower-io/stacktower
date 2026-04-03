package php

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// ComposerMatcher implements constraint matching for Composer version constraints.
// Composer uses semver-like syntax with:
// - Exact: 1.0.0
// - Range operators: >=, <=, >, <, !=
// - Caret: ^1.2.3 (semver compatible)
// - Tilde: ~1.2.3 (next significant release)
// - Wildcard: 1.2.*
// - Hyphen: 1.0.0 - 2.0.0
// - OR: ||
// - AND: space or comma
type ComposerMatcher struct{}

// Ensure ComposerMatcher implements ConstraintParser
var _ deps.ConstraintParser = ComposerMatcher{}

// composerVersion holds a parsed Composer version
type composerVersion struct {
	original  string
	major     int
	minor     int
	patch     int
	stability string // dev, alpha, beta, RC
	valid     bool
	hasMinor  bool
	hasPatch  bool
}

var (
	// Matches Composer version: v1.2.3, 1.2.3-beta, 1.2.3@dev
	composerVersionRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[-.]?(dev|alpha|beta|rc|stable)[\d.]*)?(?:@(dev|alpha|beta|rc|stable))?$`)

	// Matches constraint operators
	composerOperatorRE = regexp.MustCompile(`^\s*(>=?|<=?|!=|\^|~|=)?\s*(.+)$`)
)

// parseComposerVersion parses a Composer version string.
func parseComposerVersion(v string) composerVersion {
	cv := composerVersion{original: v}
	v = strings.TrimSpace(v)
	v = strings.ToLower(v)

	m := composerVersionRE.FindStringSubmatch(v)
	if m == nil {
		return cv
	}

	cv.valid = true
	cv.major, _ = strconv.Atoi(m[1])
	if m[2] != "" {
		cv.minor, _ = strconv.Atoi(m[2])
		cv.hasMinor = true
	}
	if m[3] != "" {
		cv.patch, _ = strconv.Atoi(m[3])
		cv.hasPatch = true
	}
	if m[4] != "" {
		cv.stability = m[4]
	}
	if m[5] != "" {
		cv.stability = m[5]
	}

	return cv
}

// ParseVersion converts a Composer version string to a PubGrub SemanticVersion.
func (ComposerMatcher) ParseVersion(version string) pubgrub.Version {
	cv := parseComposerVersion(version)
	if !cv.valid {
		return nil
	}
	semVer, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", cv.major, cv.minor, cv.patch))
	if err != nil {
		return pubgrub.SimpleVersion(version)
	}
	return semVer
}

// ParseConstraint converts a Composer version constraint to a PubGrub Condition.
func (ComposerMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	constraint = strings.TrimSpace(constraint)

	// Handle wildcard
	if constraint == "*" {
		vs, _ := pubgrub.ParseVersionRange("*")
		return pubgrub.NewVersionSetCondition(vs)
	}

	// Handle "||" (OR) - union of ranges
	if strings.Contains(constraint, "||") {
		parts := strings.Split(constraint, "||")
		var ranges []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if r := composerAndConstraintToRange(part); r != "" {
				ranges = append(ranges, r)
			}
		}
		if len(ranges) == 0 {
			return nil
		}
		// Join with || for union
		rangeStr := strings.Join(ranges, " || ")
		versionSet, err := pubgrub.ParseVersionRange(rangeStr)
		if err != nil {
			return nil
		}
		return pubgrub.NewVersionSetCondition(versionSet)
	}

	// Single AND constraint group
	rangeStr := composerAndConstraintToRange(constraint)
	if rangeStr == "" {
		return nil
	}

	versionSet, err := pubgrub.ParseVersionRange(rangeStr)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(versionSet)
}

// composerAndConstraintToRange handles space/comma-separated AND constraints.
func composerAndConstraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)

	// Handle hyphen range: "1.0.0 - 2.0.0"
	if strings.Contains(constraint, " - ") {
		parts := strings.SplitN(constraint, " - ", 2)
		if len(parts) == 2 {
			from := parseComposerVersion(strings.TrimSpace(parts[0]))
			to := parseComposerVersion(strings.TrimSpace(parts[1]))
			if from.valid && to.valid {
				// Hyphen range: >=from, <=to (inclusive)
				return fmt.Sprintf(">=%d.%d.%d, <=%d.%d.%d",
					from.major, from.minor, from.patch,
					to.major, to.minor, to.patch)
			}
		}
	}

	// Handle space-separated (AND) constraints
	// Replace comma with space and split
	constraint = strings.ReplaceAll(constraint, ",", " ")
	parts := strings.Fields(constraint)

	if len(parts) > 1 {
		var ranges []string
		for _, part := range parts {
			if r := composerSingleConstraintToRange(part); r != "" {
				ranges = append(ranges, r)
			}
		}
		return strings.Join(ranges, ", ")
	}

	return composerSingleConstraintToRange(constraint)
}

// composerSingleConstraintToRange converts a single Composer constraint to PubGrub syntax.
func composerSingleConstraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}

	// Handle wildcard: 1.*, 1.2.*
	if strings.HasSuffix(constraint, ".*") || strings.HasSuffix(constraint, ".x") {
		prefix := strings.TrimSuffix(strings.TrimSuffix(constraint, ".*"), ".x")
		cv := parseComposerVersion(prefix)
		if cv.valid {
			if !cv.hasMinor {
				// 1.* -> >=1.0.0, <2.0.0
				return fmt.Sprintf(">=%d.0.0, <%d.0.0", cv.major, cv.major+1)
			}
			// 1.2.* -> >=1.2.0, <1.3.0
			return fmt.Sprintf(">=%d.%d.0, <%d.%d.0", cv.major, cv.minor, cv.major, cv.minor+1)
		}
	}

	m := composerOperatorRE.FindStringSubmatch(constraint)
	if m == nil {
		return ""
	}

	op := m[1]
	versionStr := m[2]
	cv := parseComposerVersion(versionStr)
	if !cv.valid {
		return ""
	}

	switch op {
	case "^":
		// Caret: allows changes that do not modify the left-most non-zero digit
		// ^1.2.3 -> >=1.2.3, <2.0.0
		// ^0.3.0 -> >=0.3.0, <0.4.0
		// ^0.0.3 -> >=0.0.3, <0.0.4
		if cv.major == 0 {
			if cv.minor == 0 {
				return fmt.Sprintf(">=%d.%d.%d, <%d.%d.%d",
					cv.major, cv.minor, cv.patch,
					cv.major, cv.minor, cv.patch+1)
			}
			return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0",
				cv.major, cv.minor, cv.patch,
				cv.major, cv.minor+1)
		}
		return fmt.Sprintf(">=%d.%d.%d, <%d.0.0",
			cv.major, cv.minor, cv.patch,
			cv.major+1)

	case "~":
		// Tilde: allows patch-level changes if minor is specified
		// ~1.2.3 -> >=1.2.3, <1.3.0
		// ~1.2 -> >=1.2.0, <2.0.0
		if !cv.hasPatch {
			// ~1.2 means next significant release at major level
			return fmt.Sprintf(">=%d.%d.0, <%d.0.0", cv.major, cv.minor, cv.major+1)
		}
		return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0",
			cv.major, cv.minor, cv.patch,
			cv.major, cv.minor+1)

	case ">=":
		return fmt.Sprintf(">=%d.%d.%d", cv.major, cv.minor, cv.patch)

	case ">":
		return fmt.Sprintf(">%d.%d.%d", cv.major, cv.minor, cv.patch)

	case "<=":
		return fmt.Sprintf("<=%d.%d.%d", cv.major, cv.minor, cv.patch)

	case "<":
		return fmt.Sprintf("<%d.%d.%d", cv.major, cv.minor, cv.patch)

	case "!=":
		return fmt.Sprintf("!=%d.%d.%d", cv.major, cv.minor, cv.patch)

	case "=", "":
		// Exact match
		return fmt.Sprintf("==%d.%d.%d", cv.major, cv.minor, cv.patch)

	default:
		return ""
	}
}
