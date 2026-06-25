package sbom

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildPURL constructs a Package URL per the purl specification.
// All name segments and the version are percent-encoded.
// See https://github.com/package-url/purl-spec
func BuildPURL(language, name, version string) string {
	purlType := purlTypeFromLanguage(language)
	if purlType == "" {
		return ""
	}

	// Maven coordinates use group:artifact; purl uses group/artifact.
	if purlType == "maven" {
		name = strings.Replace(name, ":", "/", 1)
	}

	encodedName := encodePURLSegments(name)

	if version != "" {
		return fmt.Sprintf("pkg:%s/%s@%s", purlType, encodedName, encodePURLPart(version))
	}
	return fmt.Sprintf("pkg:%s/%s", purlType, encodedName)
}

// encodePURLSegments percent-encodes each "/"-separated segment of a purl
// name (slashes separate namespace segments and are kept literal).
func encodePURLSegments(name string) string {
	segments := strings.Split(name, "/")
	for i, seg := range segments {
		segments[i] = encodePURLPart(seg)
	}
	return strings.Join(segments, "/")
}

// encodePURLPart percent-encodes a single purl segment or version.
// In addition to standard path-segment escaping, '@' and ':' must be encoded
// because they act as separators in the purl grammar.
func encodePURLPart(s string) string {
	escaped := url.PathEscape(s)
	escaped = strings.ReplaceAll(escaped, "@", "%40")
	escaped = strings.ReplaceAll(escaped, ":", "%3A")
	return escaped
}

func purlTypeFromLanguage(language string) string {
	switch language {
	case "python":
		return "pypi"
	case "javascript":
		return "npm"
	case "rust":
		return "cargo"
	case "go", "golang":
		return "golang"
	case "ruby":
		return "gem"
	case "php":
		return "composer"
	case "java":
		return "maven"
	default:
		return ""
	}
}
