package devcontainer

import (
	"crypto/sha256"
	"encoding/base32"
	"path/filepath"
	"strings"

	"github.com/griffithind/dcx/internal/common"
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

// ComputeDevContainerID creates a DevContainerID from workspace path and config.
// The ProjectName is derived from the devcontainer.json name field (sanitized).
func ComputeDevContainerID(workspacePath string, cfg *DevContainerConfig) *DevContainerID {
	id := ComputeID(workspacePath)

	name := filepath.Base(workspacePath)
	if cfg != nil && cfg.Name != "" {
		name = cfg.Name
	}

	// ProjectName = sanitized devcontainer name (if set)
	projectName := ""
	if cfg != nil && cfg.Name != "" {
		projectName = common.SanitizeProjectName(cfg.Name)
	}

	// SSH host: prefer sanitized project name, otherwise use ID
	sshHost := id
	if projectName != "" {
		sshHost = projectName
	}
	sshHost = sshHost + common.SSHHostSuffix

	return &DevContainerID{
		ID:          id,
		Name:        name,
		ProjectName: projectName,
		SSHHost:     sshHost,
	}
}

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Deprecated: Use common.SanitizeProjectName instead. This is kept for backward compatibility.
var SanitizeProjectName = common.SanitizeProjectName
