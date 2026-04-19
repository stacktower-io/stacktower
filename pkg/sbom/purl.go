package sbom

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildPURL constructs a Package URL per the purl specification.
// See https://github.com/package-url/purl-spec
func BuildPURL(language, name, version string) string {
	purlType := purlTypeFromLanguage(language)
	if purlType == "" {
		return ""
	}

	// Handle scoped npm packages: @scope/name -> %40scope/name
	encodedName := name
	if purlType == "npm" && strings.HasPrefix(name, "@") {
		parts := strings.SplitN(name[1:], "/", 2)
		if len(parts) == 2 {
			encodedName = fmt.Sprintf("%%40%s/%s", url.PathEscape(parts[0]), url.PathEscape(parts[1]))
		}
	}

	// Handle Maven group:artifact -> group/artifact
	if purlType == "maven" && strings.Contains(name, ":") {
		parts := strings.SplitN(name, ":", 2)
		encodedName = fmt.Sprintf("%s/%s", url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	}

	if version != "" {
		return fmt.Sprintf("pkg:%s/%s@%s", purlType, encodedName, version)
	}
	return fmt.Sprintf("pkg:%s/%s", purlType, encodedName)
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
