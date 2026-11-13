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

	// ResourceName is the Docker name of the environment: the container
	// name, or the compose project ("<sanitized name>-<id prefix>", unique
	// per workspace, unless the compose files name the project).
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

// NewIdentity derives a workspace's identity. It is the only place the
// identity is computed: resolution stores it on ResolvedDevContainer, and
// commands that must work without resolving (stop, down, ssh) use
// LoadIdentity.
//
//   - Name: devcontainer.json "name", or the workspace directory name.
//   - SSHHost: "<sanitized name>.dcx", or "<id>.dcx" without a name.
//   - ResourceName: "<sanitized name>-<id prefix>", except that a compose
//     plan whose compose files set a top-level "name" uses that project.
//
// cfg may be nil (unreadable config); configPath locates compose files.
func NewIdentity(workspacePath string, cfg *DevContainerConfig, configPath string) Identity {
	id := ComputeID(workspacePath)
	ids := Identity{WorkspaceID: id, Name: filepath.Base(workspacePath), SSHHost: id + SSHHostSuffix}
	if cfg != nil && cfg.Name != "" {
		ids.Name = cfg.Name
		if slug := SanitizeProjectName(cfg.Name); slug != "" {
			ids.SSHHost = slug + SSHHostSuffix
		}
	}
	ids.ResourceName = ResourceName(ids.Name, id)
	if cfg != nil && cfg.IsComposePlan() {
		if explicit := SanitizeProjectName(getExplicitProjectName(composeFilePaths(cfg, configPath))); explicit != "" {
			ids.ResourceName = explicit
		}
	}
	return ids
}

// LoadIdentity reads devcontainer.json (without resolving features or
// touching Docker) and returns the workspace's identity; with an
// unreadable config it falls back to the path-derived identity.
func LoadIdentity(workspacePath, configPath string) Identity {
	cfg, path, err := Load(workspacePath, configPath)
	if err != nil {
		cfg = nil
	}
	return NewIdentity(workspacePath, cfg, path)
}

// composeFilePaths returns the config's compose files as absolute paths.
func composeFilePaths(cfg *DevContainerConfig, configPath string) []string {
	dir := filepath.Dir(configPath)
	paths := make([]string, len(cfg.DockerComposeFile))
	for i, f := range cfg.DockerComposeFile {
		if filepath.IsAbs(f) {
			paths[i] = f
		} else {
			paths[i] = filepath.Join(dir, f)
		}
	}
	return paths
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
