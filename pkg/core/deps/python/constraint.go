package python

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/contriboss/pubgrub-go"

	"github.com/stacktower-io/stacktower/pkg/core/deps"
)

// PEP440Matcher implements constraint matching for Python's PEP 440 version specifiers.
// It supports common operators: ==, !=, <, <=, >, >=, ~=
// Multiple constraints can be combined with commas (e.g., ">=1.0,<2.0").
type PEP440Matcher struct{}

// Ensure PEP440Matcher implements ConstraintParser
var _ deps.ConstraintParser = PEP440Matcher{}

// BestMatch finds the highest version from candidates that satisfies the constraint.
// Returns empty string if no version matches or constraint is empty/invalid.
func (PEP440Matcher) BestMatch(constraint string, candidates []string) string {
	if constraint == "" || len(candidates) == 0 {
		return ""
	}

	// Parse constraint into individual specifiers
	specs := parseConstraint(constraint)
	if len(specs) == 0 {
		return ""
	}

	// Filter candidates to only valid versions (exclude pre-releases, dev, etc.)
	var validVersions []parsedVersion
	for _, v := range candidates {
		pv := parseVersion(v)
		if pv.valid && !pv.prerelease {
			validVersions = append(validVersions, pv)
		}
	}

	// Sort versions descending (highest first)
	sort.Slice(validVersions, func(i, j int) bool {
		return compareVersions(validVersions[i], validVersions[j]) > 0
	})

	// Find the first (highest) version that satisfies all constraints
	for _, pv := range validVersions {
		if satisfiesAll(pv, specs) {
			return pv.original
		}
	}

	return ""
}

// specifier represents a single version constraint like ">=1.0"
type specifier struct {
	op      string        // ==, !=, <, <=, >, >=, ~=
	version parsedVersion // The version to compare against
}

// parsedVersion holds a parsed PEP 440 version. It is a pragmatic subset of
// the full specification: epoch, a three-component release tuple, one
// pre-release segment (a/b/rc), one post-release segment, one dev segment,
// and a local version label.
type parsedVersion struct {
	original   string
	epoch      int // "1!2.0" -> epoch 1 (default 0)
	major      int
	minor      int
	patch      int
	preType    string // normalized: "a", "b", or "rc"
	preNum     int    // "1.0a2" -> 2
	hasPre     bool
	postNum    int // "1.0.post1" -> 1
	hasPost    bool
	devNum     int // "1.0.dev3" -> 3
	hasDev     bool
	local      string // "1.0+local.tag" -> "local.tag" (ignored for ordering)
	prerelease bool   // true when a pre-release or dev segment is present
	valid      bool
}

var (
	// Matches constraint operators and version
	specRE = regexp.MustCompile(`^\s*(~=|===?|!=|<=?|>=?)\s*([^\s,]+)\s*$`)

	// Matches PEP 440 version components:
	// [epoch!]release[{a|b|rc}N][.postN][.devN][+local]
	versionRE = regexp.MustCompile(`^(?:(\d+)!)?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[-._]?(a|alpha|b|beta|c|rc|pre|preview)[-._]?(\d*))?(?:[-._]?(post|rev|r)[-._]?(\d*))?(?:[-._]?(dev)[-._]?(\d*))?(?:\+([a-z0-9]+(?:[-._][a-z0-9]+)*))?$`)
)

// normalizePreType maps PEP 440 pre-release spellings to canonical forms:
// alpha -> a, beta -> b, c/pre/preview -> rc.
func normalizePreType(t string) string {
	switch t {
	case "alpha":
		return "a"
	case "beta":
		return "b"
	case "c", "pre", "preview":
		return "rc"
	default:
		return t
	}
}

// parseConstraint splits a constraint string into individual specifiers.
// Example: ">=1.0,<2.0" -> [specifier{op:">=", version:1.0}, specifier{op:"<", version:2.0}]
func parseConstraint(constraint string) []specifier {
	var specs []specifier
	parts := strings.Split(constraint, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		m := specRE.FindStringSubmatch(part)
		if m == nil {
			// Invalid specifier, skip
			continue
		}

		op := m[1]
		verStr := m[2]

		pv := parseVersion(verStr)
		if !pv.valid {
			continue
		}

		specs = append(specs, specifier{op: op, version: pv})
	}

	return specs
}

// parseVersion parses a version string into components.
func parseVersion(v string) parsedVersion {
	pv := parsedVersion{original: v}

	// Normalize: remove leading 'v', convert underscores
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	v = strings.ToLower(v)

	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return pv
	}

	pv.valid = true
	if m[1] != "" {
		pv.epoch, _ = strconv.Atoi(m[1])
	}
	pv.major, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		pv.minor, _ = strconv.Atoi(m[3])
	}
	if m[4] != "" {
		pv.patch, _ = strconv.Atoi(m[4])
	}
	if m[5] != "" {
		pv.hasPre = true
		pv.preType = normalizePreType(m[5])
		if m[6] != "" {
			pv.preNum, _ = strconv.Atoi(m[6])
		}
	}
	if m[7] != "" {
		pv.hasPost = true
		if m[8] != "" {
			pv.postNum, _ = strconv.Atoi(m[8])
		}
	}
	if m[9] != "" {
		pv.hasDev = true
		if m[10] != "" {
			pv.devNum, _ = strconv.Atoi(m[10])
		}
	}
	pv.local = m[11]
	// Post-releases and local versions are NOT pre-releases: pip installs
	// them by default. Only pre (aN/bN/rcN) and dev segments are.
	pv.prerelease = pv.hasPre || pv.hasDev

	return pv
}

// segmentRank orders the version segment kinds per PEP 440 for an identical
// release tuple: dev < pre < final < post.
func segmentRank(v parsedVersion) int {
	switch {
	case v.hasPre:
		return 1
	case v.hasPost:
		return 3
	case v.hasDev:
		return 0
	default:
		return 2
	}
}

// preTypeRank orders pre-release types: a < b < rc.
func preTypeRank(t string) int {
	switch t {
	case "a":
		return 0
	case "b":
		return 1
	default: // rc
		return 2
	}
}

// compareVersions compares two versions with PEP 440 ordering:
// epoch first, then the release tuple, then segment kind
// (dev < pre (a < b < rc) < final < post). Local version labels are
// ignored for ordering (pragmatic subset of the spec).
// Returns: >0 if a > b, <0 if a < b, 0 if equal
func compareVersions(a, b parsedVersion) int {
	if a.epoch != b.epoch {
		return a.epoch - b.epoch
	}
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	if a.patch != b.patch {
		return a.patch - b.patch
	}
	if ra, rb := segmentRank(a), segmentRank(b); ra != rb {
		return ra - rb
	}
	// Same segment kind: compare within the segment.
	if a.hasPre && b.hasPre {
		if ta, tb := preTypeRank(a.preType), preTypeRank(b.preType); ta != tb {
			return ta - tb
		}
		if a.preNum != b.preNum {
			return a.preNum - b.preNum
		}
	}
	if a.hasPost && b.hasPost && a.postNum != b.postNum {
		return a.postNum - b.postNum
	}
	// A dev sub-segment sorts below the same version without one
	// (e.g. 1.0a1.dev1 < 1.0a1, 1.0.post1.dev1 < 1.0.post1).
	if a.hasDev != b.hasDev {
		if a.hasDev {
			return -1
		}
		return 1
	}
	if a.hasDev && a.devNum != b.devNum {
		return a.devNum - b.devNum
	}
	return 0
}

// satisfiesAll checks if a version satisfies all specifiers.
func satisfiesAll(v parsedVersion, specs []specifier) bool {
	for _, s := range specs {
		if !satisfies(v, s) {
			return false
		}
	}
	return true
}

// satisfies checks if a version satisfies a single specifier.
func satisfies(v parsedVersion, s specifier) bool {
	cmp := compareVersions(v, s.version)

	switch s.op {
	case "==", "===":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "~=":
		// Compatible release: ~=X.Y.Z means >=X.Y.Z,<X.(Y+1).0
		// ~=X.Y means >=X.Y,<(X+1).0
		if cmp < 0 {
			return false
		}
		// Upper bound: increment the second-to-last component
		if s.version.patch > 0 || versionHasPatch(s.version.original) {
			// ~=1.4.2 means >=1.4.2,<1.5.0
			return v.major == s.version.major && v.minor == s.version.minor
		}
		// ~=1.4 means >=1.4,<2.0
		return v.major == s.version.major
	default:
		return false
	}
}

// versionHasPatch checks if the original version string has a patch component.
func versionHasPatch(v string) bool {
	parts := strings.Split(v, ".")
	return len(parts) >= 3
}

// =============================================================================
// PubGrub ConstraintParser implementation
// =============================================================================

// pep440Version is a PubGrub Version that preserves the original version string
// so that registry fetches use the exact form returned by ListVersions (e.g.
// "5.0.0a2" rather than the stripped "5.0.0"). Sort() applies PEP 440 ordering:
// pre-release versions come before the corresponding stable release.
type pep440Version struct {
	original string
	parsed   parsedVersion
}

func (v pep440Version) String() string { return v.original }

func (v pep440Version) Sort(other pubgrub.Version) int {
	var op parsedVersion
	if o, ok := other.(pep440Version); ok {
		op = o.parsed
	} else {
		op = parseVersion(other.String())
	}
	// compareVersions implements full PEP 440 ordering including
	// dev/pre/post segments, so no extra tiebreak is needed here.
	return compareVersions(v.parsed, op)
}

// makePEP440Version builds a pep440Version from an already-parsed parsedVersion.
func makePEP440Version(pv parsedVersion) pep440Version {
	return pep440Version{original: pv.original, parsed: pv}
}

// ParseVersion converts a Python version string to a PubGrub Version.
// The original string is preserved so downstream fetches use the exact registry form.
func (PEP440Matcher) ParseVersion(version string) pubgrub.Version {
	pv := parseVersion(version)
	if !pv.valid {
		return nil
	}
	return makePEP440Version(pv)
}

// ParseConstraint converts a PEP 440 constraint to a PubGrub Condition.
// Returns nil if the constraint is empty or cannot be parsed.
// Version set bounds are built from pep440Version values so that range
// containment checks use the same Sort() logic as the candidate versions.
func (PEP440Matcher) ParseConstraint(constraint string) pubgrub.Condition {
	if constraint == "" {
		return nil
	}

	specs := parseConstraint(constraint)
	if len(specs) == 0 {
		return nil
	}

	combined := pubgrub.FullVersionSet()
	for _, s := range specs {
		v := makePEP440Version(s.version)
		var specSet pubgrub.VersionSet

		switch s.op {
		case "==", "===":
			specSet = pubgrub.NewVersionRangeSet(v, true, v, true)
		case "!=":
			specSet = pubgrub.NewVersionRangeSet(v, true, v, true).Complement()
		case ">=":
			specSet = pubgrub.NewLowerBoundVersionSet(v, true)
		case ">":
			specSet = pubgrub.NewLowerBoundVersionSet(v, false)
		case "<=":
			specSet = pubgrub.NewUpperBoundVersionSet(v, true)
		case "<":
			// When bound is a plain final version (e.g., <1.0.0), exclude
			// prereleases of that version too. Users expect <1.0.0 to mean
			// "before the 1.0 release series", not "anything that sorts
			// before 1.0.0". Achieve this by using a synthetic dev0
			// prerelease as the bound.
			if !s.version.hasPre && !s.version.hasPost && !s.version.hasDev {
				devBound := pep440Version{
					original: fmt.Sprintf("%d.%d.%d.dev0", s.version.major, s.version.minor, s.version.patch),
					parsed: parsedVersion{
						epoch:      s.version.epoch,
						major:      s.version.major,
						minor:      s.version.minor,
						patch:      s.version.patch,
						hasDev:     true,
						prerelease: true,
						valid:      true,
					},
				}
				specSet = pubgrub.NewUpperBoundVersionSet(devBound, false)
			} else {
				specSet = pubgrub.NewUpperBoundVersionSet(v, false)
			}
		case "~=":
			// ~=X.Y.Z → >=X.Y.Z, <X.(Y+1).0
			// ~=X.Y   → >=X.Y.0, <(X+1).0.0
			var ceil pep440Version
			if s.version.patch > 0 || versionHasPatch(s.version.original) {
				ceil = pep440Version{
					original: fmt.Sprintf("%d.%d.0", s.version.major, s.version.minor+1),
					parsed: parsedVersion{
						epoch: s.version.epoch, major: s.version.major, minor: s.version.minor + 1, valid: true,
					},
				}
			} else {
				ceil = pep440Version{
					original: fmt.Sprintf("%d.0.0", s.version.major+1),
					parsed: parsedVersion{
						epoch: s.version.epoch, major: s.version.major + 1, valid: true,
					},
				}
			}
			specSet = pubgrub.NewVersionRangeSet(v, true, ceil, false)
		default:
			continue
		}

		combined = combined.Intersection(specSet)
	}

	return pubgrub.NewVersionSetCondition(combined)
}
