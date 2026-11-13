package devcontainer

import "strings"

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Docker requires lowercase alphanumeric with hyphens/underscores, starting with a letter.
//
// It is the only project name sanitizer in dcx.
//
// Rules:
//   - Converts to lowercase
//   - Replaces spaces with underscores
//   - Removes characters that are not alphanumeric, hyphen, or underscore
//   - Prefixes with "dcx_" if name starts with a number
func SanitizeProjectName(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with underscores and filter invalid characters
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if r == ' ' {
			result.WriteRune('_')
		}
		// Skip other characters
	}

	sanitized := result.String()
	if sanitized == "" {
		return ""
	}

	// Ensure starts with a letter (Docker requirement)
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "dcx_" + sanitized
	}

	return sanitized
}
