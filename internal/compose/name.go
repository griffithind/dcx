// Package compose provides utilities for working with Docker Compose files.
package compose

import (
	"os"

	"gopkg.in/yaml.v3"
)

// GetExplicitProjectName checks if any of the compose files has an explicit "name" field.
// Returns the name if found, empty string otherwise.
// This is useful to distinguish between an explicitly set name and one auto-derived from directory.
func GetExplicitProjectName(files []string) string {
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}

		if name, ok := raw["name"].(string); ok && name != "" {
			return name
		}
	}
	return ""
}
