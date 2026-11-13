package devcontainer

import (
	"crypto/sha256"
	"encoding/base32"
	"path/filepath"
	"strings"
)

// Identity names a workspace: how dcx finds its containers, names its
// Docker resources and aliases it for SSH.
type Identity struct {
	// WorkspaceID is the stable workspace identifier (hash of its path).
	WorkspaceID string

	// Name is the human-readable name (devcontainer.json name, or the
	// directory name).
	Name string

	// ResourceName is the Docker container name and default compose
	// project: "<sanitized name>-<id prefix>", unique per workspace.
	ResourceName string

	// SSHHost is the preferred SSH alias ("<name>.dcx", or "<id>.dcx").
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
	realPath, err := realPath(workspacePath)
	if err != nil {
		// Fall back to the original path if we can't resolve
		realPath = workspacePath
	}

	// Normalize the path
	realPath = normalizePath(realPath)

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

// ComputeIdentity derives a workspace's identity from its path and config
// (cfg may be nil when the config cannot be read).
func ComputeIdentity(workspacePath string, cfg *DevContainerConfig) *Identity {
	id := ComputeID(workspacePath)

	name := filepath.Base(workspacePath)
	if cfg != nil && cfg.Name != "" {
		name = cfg.Name
	}

	// SSH host alias: the readable "<name>.dcx" users type, falling back
	// to the ID when there is no name.
	sshHost := id
	if cfg != nil && cfg.Name != "" {
		if slug := SanitizeProjectName(cfg.Name); slug != "" {
			sshHost = slug
		}
	}
	sshHost = sshHost + SSHHostSuffix

	return &Identity{
		WorkspaceID:  id,
		Name:         name,
		ResourceName: ResourceName(name, id),
		SSHHost:      sshHost,
	}
}

// resourceIDLength is how many characters of the workspace ID are appended
// to Docker resource names.
const resourceIDLength = 8

// ResourceName returns the Docker container name / default compose project
// name for a workspace: the sanitized display name plus a prefix of the
// workspace ID. Deriving it from the name alone made two checkouts (clones,
// worktrees) with the same "name" collide on "container name already in
// use".
func ResourceName(displayName, workspaceID string) string {
	suffix := workspaceID
	if len(suffix) > resourceIDLength {
		suffix = suffix[:resourceIDLength]
	}
	slug := SanitizeProjectName(displayName)
	if slug == "" {
		slug = "dcx"
	}
	if suffix == "" {
		return slug
	}
	return slug + "-" + suffix
}

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.

// SSHHostSuffix is appended to a workspace's name to form its SSH alias.
const SSHHostSuffix = ".dcx"

// realPath returns the absolute path with symlinks resolved.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// normalizePath cleans path, uses forward slashes and drops a trailing one.
func normalizePath(path string) string {
	return strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), "/")
}
