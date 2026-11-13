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
	// WorkspaceID is the stable workspace identifier (hash of its path, and
	// of the config file when it is not in a default location).
	WorkspaceID string

	// Name is the human-readable name (devcontainer.json name, or the
	// directory name).
	Name string

	// ResourceName is the Docker name of the environment: the container
	// name, or the compose project ("<sanitized name>-<id prefix>", unique
	// per workspace; a compose file's top-level name replaces the name
	// part).
	ResourceName string

	// SSHHost is the preferred SSH alias ("<name>.dcx", or "<id>.dcx").
	SSHHost string
}

// ComputeID generates a stable workspace identifier from the workspace path:
// base32(sha256(realpath(workspace_root)))[0:12]. It is the ID of a
// workspace using a default config location; see WorkspaceID.
func ComputeID(workspacePath string) string {
	return hashID(canonicalPath(workspacePath))
}

// WorkspaceID returns the ID of the workspace at workspacePath when it uses
// the devcontainer.json at configPath. For the default locations
// (.devcontainer/devcontainer.json, .devcontainer.json, or "") it is
// ComputeID; any other config (.devcontainer/<name>/devcontainer.json,
// --config) gets its own ID, so two configs of one folder are two
// environments rather than recreating each other. This is the ID labels,
// Docker names and ${devcontainerId} use.
func WorkspaceID(workspacePath, configPath string) string {
	root := canonicalPath(workspacePath)
	if configPath == "" {
		return hashID(root)
	}
	rel, err := filepath.Rel(root, canonicalPath(configPath))
	if err != nil {
		rel = canonicalPath(configPath)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".devcontainer/devcontainer.json" || rel == ".devcontainer.json" {
		return hashID(root)
	}
	return hashID(root + "\x00" + rel)
}

// canonicalPath resolves symlinks and normalizes path (falling back to the
// path as given when it can't be resolved).
func canonicalPath(path string) string {
	p, err := realPath(path)
	if err != nil {
		p = path
	}
	return normalizePath(p)
}

// hashID is base32(sha256(s))[0:12], lower case.
func hashID(s string) string {
	hash := sha256.Sum256([]byte(s))
	encoded := strings.ToLower(base32.StdEncoding.EncodeToString(hash[:]))
	return encoded[:12]
}

// NewIdentity derives a workspace's identity. It is the only place the
// identity is computed: resolution stores it on ResolvedDevContainer, and
// commands that must work without resolving (stop, down, ssh) use
// LoadIdentity.
//
//   - Name: devcontainer.json "name", or the workspace directory name.
//   - SSHHost: "<sanitized name>.dcx", or "<id>.dcx" without a name.
//   - ResourceName: "<sanitized name>-<id prefix>"; for a compose plan
//     whose compose files set a top-level "name", "<that name>-<id prefix>".
//
// cfg may be nil (unreadable config); configPath locates compose files.
func NewIdentity(workspacePath string, cfg *DevContainerConfig, configPath string) Identity {
	id := WorkspaceID(workspacePath, configPath)
	ids := Identity{WorkspaceID: id, Name: filepath.Base(workspacePath), SSHHost: id + SSHHostSuffix}
	if cfg != nil && cfg.Name != "" {
		ids.Name = cfg.Name
		if slug := SanitizeProjectName(cfg.Name); slug != "" {
			ids.SSHHost = slug + SSHHostSuffix
		}
	}
	ids.ResourceName = ResourceName(ids.Name, id)
	// A compose file's top-level "name" names the project, still suffixed
	// with the workspace ID: two checkouts of one repository must not share
	// (and take over) one compose project.
	if cfg != nil && cfg.IsComposePlan() {
		if explicit := getExplicitProjectName(composeFilePaths(cfg, configPath)); SanitizeProjectName(explicit) != "" {
			ids.ResourceName = ResourceName(explicit, id)
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
