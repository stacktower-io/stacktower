package python

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// extractPyprojectName reads the project name from pyproject.toml in the given directory.
// It checks both [tool.poetry.name] and [project.name] sections.
func extractPyprojectName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if err != nil {
		return ""
	}
	var pyproject struct {
		Tool struct {
			Poetry struct {
				Name string `toml:"name"`
			} `toml:"poetry"`
		} `toml:"tool"`
		Project struct {
			Name string `toml:"name"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(data, &pyproject); err != nil {
		return ""
	}
	if pyproject.Tool.Poetry.Name != "" {
		return pyproject.Tool.Poetry.Name
	}
	return pyproject.Project.Name
}

// extractConstraint extracts version constraint from a dependency value.
// The value can be a string (e.g., ">=1.0", "^1.0") or a map with "version" key.
// This handles both poetry.lock and pyproject.toml dependency formats.
func extractConstraint(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if version, ok := val["version"].(string); ok {
			return version
		}
	}
	return ""
}
