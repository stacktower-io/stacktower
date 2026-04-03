package rust

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/matzehuels/stacktower/pkg/core/deps"
)

// CargoMatcher implements constraint matching for Cargo's semver specifiers.
// Cargo uses semver with caret as the default operator.
// Supports: ^, ~, >=, <=, >, <, =, *, and comma-separated ranges.
type CargoMatcher struct{}

// Ensure CargoMatcher implements ConstraintParser
var _ deps.ConstraintParser = CargoMatcher{}

// cargoVersion holds a parsed semantic version
type cargoVersion struct {
	original   string
	major      int
	minor      int
	patch      int
	prerelease string
	valid      bool
	// Track which parts were explicitly specified
	hasMinor bool
	hasPatch bool
}

var (
	// Matches semver with optional parts: 1, 1.2, 1.2.3, 1.2.3-beta
	cargoVersionRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([\w.]+))?(?:\+[\w.]+)?$`)

	// Matches constraint operators
	cargoOperatorRE = regexp.MustCompile(`^\s*(>=?|<=?|=|\^|~)?\s*(.+)$`)
)

// parseCargoVersion parses a Cargo version string.
func parseCargoVersion(v string) cargoVersion {
	cv := cargoVersion{original: v}
	v = strings.TrimSpace(v)

	m := cargoVersionRE.FindStringSubmatch(v)
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
	cv.prerelease = m[4]

	return cv
}

// ParseVersion converts a Cargo version string to a PubGrub SemanticVersion.
func (CargoMatcher) ParseVersion(version string) pubgrub.Version {
	cv := parseCargoVersion(version)
	if !cv.valid {
		return nil
	}
	semVer, err := pubgrub.ParseSemanticVersion(fmt.Sprintf("%d.%d.%d", cv.major, cv.minor, cv.patch))
	if err != nil {
		return pubgrub.SimpleVersion(version)
	}
	return semVer
}

// ParseConstraint converts a Cargo version requirement to a PubGrub Condition.
// Cargo requirements: https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html
func (CargoMatcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	constraint = strings.TrimSpace(constraint)

	// Handle wildcard
	if constraint == "*" {
		vs, _ := pubgrub.ParseVersionRange("*")
		return pubgrub.NewVersionSetCondition(vs)
	}

	// Handle comma-separated (AND) constraints
	if strings.Contains(constraint, ",") {
		parts := strings.Split(constraint, ",")
		var ranges []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if r := cargoSingleConstraintToRange(part); r != "" {
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
	rangeStr := cargoSingleConstraintToRange(constraint)
	if rangeStr == "" {
		return nil
	}

	versionSet, err := pubgrub.ParseVersionRange(rangeStr)
	if err != nil {
		return nil
	}
	return pubgrub.NewVersionSetCondition(versionSet)
}

// cargoSingleConstraintToRange converts a single Cargo constraint to PubGrub range syntax.
func cargoSingleConstraintToRange(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return ""
	}

	// Handle wildcard ranges: 1.*, 1.2.*
	if strings.HasSuffix(constraint, ".*") {
		prefix := strings.TrimSuffix(constraint, ".*")
		cv := parseCargoVersion(prefix)
		if cv.valid {
			if !cv.hasMinor {
				// 1.* -> >=1.0.0, <2.0.0
				return fmt.Sprintf(">=%d.0.0, <%d.0.0", cv.major, cv.major+1)
			}
			// 1.2.* -> >=1.2.0, <1.3.0
			return fmt.Sprintf(">=%d.%d.0, <%d.%d.0", cv.major, cv.minor, cv.major, cv.minor+1)
		}
	}

	m := cargoOperatorRE.FindStringSubmatch(constraint)
	if m == nil {
		return ""
	}

	op := m[1]
	versionStr := m[2]
	cv := parseCargoVersion(versionStr)
	if !cv.valid {
		return ""
	}

	// Default operator in Cargo is caret (^)
	if op == "" {
		op = "^"
	}

	switch op {
	case "^":
		// Caret: allows SemVer compatible updates
		// ^1.2.3 := >=1.2.3, <2.0.0
		// ^0.2.3 := >=0.2.3, <0.3.0
		// ^0.0.3 := >=0.0.3, <0.0.4
		// ^1.2   := >=1.2.0, <2.0.0
		// ^0.0   := >=0.0.0, <0.1.0
		// ^0     := >=0.0.0, <1.0.0
		if cv.major == 0 {
			if !cv.hasMinor || cv.minor == 0 {
				if !cv.hasPatch || cv.patch == 0 {
					// ^0 or ^0.0 or ^0.0.0 -> <1.0.0 or <0.1.0
					if !cv.hasMinor {
						return fmt.Sprintf(">=%d.0.0, <%d.0.0", cv.major, cv.major+1)
					}
					return fmt.Sprintf(">=%d.%d.0, <%d.%d.0", cv.major, cv.minor, cv.major, cv.minor+1)
				}
				// ^0.0.x -> exact patch
				return fmt.Sprintf(">=%d.%d.%d, <%d.%d.%d",
					cv.major, cv.minor, cv.patch,
					cv.major, cv.minor, cv.patch+1)
			}
			// ^0.x.y -> minor is significant
			return fmt.Sprintf(">=%d.%d.%d, <%d.%d.0",
				cv.major, cv.minor, cv.patch,
				cv.major, cv.minor+1)
		}
		return fmt.Sprintf(">=%d.%d.%d, <%d.0.0",
			cv.major, cv.minor, cv.patch,
			cv.major+1)

	case "~":
		// Tilde: minimal changes
		// ~1.2.3 := >=1.2.3, <1.3.0
		// ~1.2   := >=1.2.0, <1.3.0
		// ~1     := >=1.0.0, <2.0.0
		if !cv.hasMinor {
			return fmt.Sprintf(">=%d.0.0, <%d.0.0", cv.major, cv.major+1)
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

	case "=":
		return fmt.Sprintf("==%d.%d.%d", cv.major, cv.minor, cv.patch)

	default:
		return ""
	}
}
