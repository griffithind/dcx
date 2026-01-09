package devcontainer

import (
	"crypto/sha256"
	"encoding/base32"
	"path/filepath"
	"strings"

	"github.com/griffithind/dcx/internal/util"
)

// DevContainerID is a lightweight identifier for quick lookups.
// Use this when you don't need the full ResolvedDevContainer.
type DevContainerID struct {
	// ID is the stable workspace identifier (hash of workspace path).
	ID string

	// Name is the human-readable name (from config or directory name).
	Name string

	// ProjectName is the sanitized project name (for compose and container naming).
	ProjectName string

	// SSHHost is the SSH hostname (name.dcx or id.dcx).
	SSHHost string
}

// ComputeID generates a stable workspace identifier from the workspace path.
// Returns base32(sha256(realpath(workspace_root)))[0:12].
//
// This is the canonical identifier used for:
// - Container labels
// - Compose project names
// - SSH hosts
// - All workspace lookups
func ComputeID(workspacePath string) string {
	// Get the real path (resolve symlinks)
	realPath, err := util.RealPath(workspacePath)
	if err != nil {
		// Fall back to the original path if we can't resolve
		realPath = workspacePath
	}

	// Normalize the path
	realPath = util.NormalizePath(realPath)

	// Compute SHA256
	hash := sha256.Sum256([]byte(realPath))

	// Encode as base32 and take first 12 characters
	encoded := base32.StdEncoding.EncodeToString(hash[:])
	encoded = strings.ToLower(encoded)

	if len(encoded) > 12 {
		encoded = encoded[:12]
	}

	return encoded
}

// ComputeName derives a workspace name from the path or config.
func ComputeName(workspacePath string, cfg *DevContainerConfig) string {
	if cfg != nil && cfg.Name != "" {
		return cfg.Name
	}
	return filepath.Base(workspacePath)
}

// ComputeDevContainerID creates a DevContainerID from workspace path and optional configurations.
func ComputeDevContainerID(workspacePath string, cfg *DevContainerConfig, dcxCfg *DcxConfig) *DevContainerID {
	id := ComputeID(workspacePath)

	name := filepath.Base(workspacePath)
	if cfg != nil && cfg.Name != "" {
		name = cfg.Name
	}

	projectName := ""
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = dcxCfg.Name
	}

	// SSH host: prefer project name if set, otherwise use ID
	sshHost := id
	if projectName != "" {
		sshHost = projectName
	}
	sshHost = sshHost + ".dcx"

	return &DevContainerID{
		ID:          id,
		Name:        name,
		ProjectName: projectName,
		SSHHost:     sshHost,
	}
}

// String returns a string representation of the DevContainerID.
func (d *DevContainerID) String() string {
	if d.ProjectName != "" {
		return d.ProjectName
	}
	return d.ID
}

// ContainerPrefix returns a prefix suitable for container naming.
func (d *DevContainerID) ContainerPrefix() string {
	if d.ProjectName != "" {
		return d.ProjectName
	}
	return "dcx-" + d.ID
}

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Docker requires lowercase alphanumeric with hyphens/underscores, starting with letter.
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
	}

	sanitized := result.String()

	// Ensure starts with a letter
	if len(sanitized) > 0 && (sanitized[0] >= '0' && sanitized[0] <= '9') {
		sanitized = "dcx-" + sanitized
	}

	return sanitized
}
