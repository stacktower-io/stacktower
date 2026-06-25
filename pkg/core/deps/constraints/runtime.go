package constraints

import (
	"regexp"
	"strings"
)

// versionConstraintRE matches version constraints (e.g., ">=3.8", "<4", "^3.10", "~3.10").
var versionConstraintRE = regexp.MustCompile(`([<>=!~^]+)\s*(\d+(?:\.\d+)*)`)
var bareVersionRE = regexp.MustCompile(`^\d+(?:\.\d+)*$`)

// NormalizeRuntimeConstraint normalizes runtime constraints to a comparable form.
// Bare versions like "1.70.0" are interpreted as minimum constraints (">=1.70.0").
func NormalizeRuntimeConstraint(constraint string) string {
	c := strings.TrimSpace(constraint)
	if c == "" {
		return ""
	}
	if bareVersionRE.MatchString(c) {
		return ">=" + c
	}
	return c
}

// ExtractMinVersion extracts the minimum version from a constraint string.
// For constraints like ">=3.8", "^3.10", "~3.9", returns the version number.
// Returns empty string if no minimum version can be determined.
func ExtractMinVersion(constraint string) string {
	constraint = NormalizeRuntimeConstraint(constraint)
	if constraint == "" {
		return ""
	}

	matches := versionConstraintRE.FindAllStringSubmatch(constraint, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		op := m[1]
		ver := m[2]

		// These operators indicate a minimum version.
		switch op {
		case ">=", "^", "~", "~=", "~>", "==":
			return ver
		case ">":
			// > means strictly greater, but we can use the version as a baseline.
			return ver
		}
	}

	return ""
}

// CheckVersionConstraint checks if a version satisfies a constraint string.
// Returns true if the version is compatible, false otherwise.
// An empty constraint is always satisfied.
//
// Supports common constraint operators:
//   - Comparison: >=, <=, <, >, ==, !=
//   - Caret (compatible): ^1.2 means >=1.2.0, <2.0.0
//   - Tilde (patch-level): ~1.2 or ~=1.2 means >=1.2.0, <1.3.0
//
// Multiple constraints can be comma-separated (AND logic).
// OR groups are supported using "|" or "||" (Composer-style), where any group may match.
func CheckVersionConstraint(version, constraint string) bool {
	constraint = NormalizeRuntimeConstraint(constraint)
	if constraint == "" {
		return true
	}

	// Support OR constraints used by Composer and others, e.g. "^8.2|^8.3" or "^8.2 || ^8.3".
	// Any group may match; constraints within a group are evaluated as AND.
	constraint = strings.ReplaceAll(constraint, "||", "|")
	for _, rawGroup := range strings.Split(constraint, "|") {
		group := strings.TrimSpace(rawGroup)
		if group == "" {
			continue
		}
		if checkConstraintGroup(version, group) {
			return true
		}
	}

	return false
}

func checkConstraintGroup(version, constraint string) bool {
	targetParts := parseVersionParts(version)

	matches := versionConstraintRE.FindAllStringSubmatch(constraint, -1)
	if len(matches) == 0 {
		// Intentionally permissive: constraints we cannot parse (e.g. "*",
		// "latest", malformed engines strings) are treated as satisfied.
		// Failing closed here would filter out every version of a package
		// whose registry metadata carries a garbage constraint, which is a
		// worse failure mode than skipping the runtime filter.
		return true
	}

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		op := m[1]
		verStr := m[2]
		verParts := parseVersionParts(verStr)

		cmp := compareVersionParts(targetParts, verParts)

		var satisfied bool
		switch op {
		case "<":
			satisfied = cmp < 0
		case "<=":
			satisfied = cmp <= 0
		case ">":
			satisfied = cmp > 0
		case ">=":
			satisfied = cmp >= 0
		case "==":
			satisfied = cmp == 0
		case "!=":
			satisfied = cmp != 0
		case "^":
			// Caret: ^X.Y.Z means >=X.Y.Z with upper bound depending on leading zeros:
			//   ^1.2.3 → >=1.2.3, <2.0.0  (major >= 1: lock major)
			//   ^0.2.3 → >=0.2.3, <0.3.0  (major == 0: lock minor)
			//   ^0.0.3 → >=0.0.3, <0.0.4  (major.minor == 0.0: lock patch)
			if cmp < 0 {
				satisfied = false
			} else if len(verParts) > 0 && verParts[0] > 0 {
				satisfied = len(targetParts) > 0 && targetParts[0] == verParts[0]
			} else if len(verParts) > 1 && verParts[1] > 0 {
				satisfied = len(targetParts) > 1 && targetParts[0] == verParts[0] && targetParts[1] == verParts[1]
			} else {
				satisfied = len(targetParts) > 2 && len(verParts) > 2 &&
					targetParts[0] == verParts[0] && targetParts[1] == verParts[1] && targetParts[2] == verParts[2]
			}
		case "~":
			// npm/Cargo tilde: locks the minor when one is specified.
			//   ~3.10   → >=3.10, <3.11
			//   ~3.10.2 → >=3.10.2, <3.11
			//   ~3      → >=3, <4
			satisfied = cmp >= 0 && compareVersionParts(targetParts, tildeUpperBound(verParts)) < 0
		case "~=", "~>":
			// Pessimistic operator (PEP 440 ~= and Ruby ~>): locks all but the
			// last specified component.
			//   ~=3.8   → >=3.8, <4.0   (so 3.11 satisfies ~=3.8)
			//   ~>2.7.1 → >=2.7.1, <2.8
			//   ~>2     → >=2, <3
			satisfied = cmp >= 0 && compareVersionParts(targetParts, pessimisticUpperBound(verParts)) < 0
		default:
			// Unknown operator: fail closed to avoid silently accepting
			// incompatible runtimes.
			satisfied = false
		}

		if !satisfied {
			return false
		}
	}

	return true
}

// tildeUpperBound returns the exclusive upper bound for an npm/Cargo-style
// tilde constraint: increment the minor when one is specified, otherwise the
// major. ~1.2(.x) → [1, 3]; ~1 → [2].
func tildeUpperBound(verParts []int) []int {
	if len(verParts) >= 2 {
		return []int{verParts[0], verParts[1] + 1}
	}
	return []int{verParts[0] + 1}
}

// pessimisticUpperBound returns the exclusive upper bound for a pessimistic
// constraint (PEP 440 ~=, Ruby ~>): drop the last specified component and
// increment the one before it. ~>2.7.1 → [2, 8]; ~=3.8 → [4]; ~>2 → [3].
func pessimisticUpperBound(verParts []int) []int {
	if len(verParts) >= 2 {
		upper := make([]int, len(verParts)-1)
		copy(upper, verParts[:len(verParts)-1])
		upper[len(upper)-1]++
		return upper
	}
	return []int{verParts[0] + 1}
}

// parseVersionParts splits a version string like "3.11" into [3, 11].
func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		var n int
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				break
			}
		}
		result[i] = n
	}
	return result
}

// compareVersionParts compares two version part slices.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersionParts(a, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
