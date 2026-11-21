// Package state detects the state of a workspace's containers from their
// Docker labels and defines the label schema.
package state

import "path/filepath"

// ContainerState is the lifecycle state of a workspace's containers.
type ContainerState string

const (
	// StateUnknown represents an indeterminate or initial state.
	StateUnknown ContainerState = ""

	// StateAbsent means no managed containers exist for this environment.
	StateAbsent ContainerState = "absent"

	// StateCreated means containers exist but the primary is not running
	// (created, or stopped).
	StateCreated ContainerState = "created"

	// StateRunning means the primary container is running.
	StateRunning ContainerState = "running"

	// StateStale means the primary container exists but its config hash
	// differs from the current computed hash.
	StateStale ContainerState = "stale"

	// StateBroken means managed containers exist but the primary is
	// missing or in an inconsistent state.
	StateBroken ContainerState = "broken"
)

// String returns the string representation of the state.
func (s ContainerState) String() string {
	if s == StateUnknown {
		return "unknown"
	}
	return string(s)
}

// ContainerInfo holds information about a container relevant to state management.
type ContainerInfo struct {
	ID             string
	Name           string
	Status         string
	Running        bool
	ConfigHash     string // Staleness key of the config the container was created from
	HashVersion    string // Algorithm version of ConfigHash ("" = legacy)
	WorkspaceID    string // Stable identifier
	Plan           string
	ComposeProject string
	PrimaryService string
	Labels         *ContainerLabels
	// Env is the container's Config.Env (image ENV, features' and the
	// config's containerEnv), from the same inspect as everything else.
	Env []string
	// User is the container's Config.User, the user `docker exec` runs as
	// by default ("" is root).
	User string
}

// IsSingleContainer returns true if this is a single-container (non-compose) devcontainer.
func (c *ContainerInfo) IsSingleContainer() bool {
	return c != nil && (c.Plan == BuildMethodImage || c.Plan == BuildMethodDockerfile)
}

// ComposeProjectOr returns the compose project name, falling back to the provided default.
func (c *ContainerInfo) ComposeProjectOr(defaultProject string) string {
	if c != nil && c.ComposeProject != "" {
		return c.ComposeProject
	}
	return defaultProject
}

// ConfigDirOr returns the config directory from labels, falling back to the provided default.
func (c *ContainerInfo) ConfigDirOr(defaultDir string) string {
	if c != nil && c.Labels != nil && c.Labels.ConfigPath != "" {
		return filepath.Dir(c.Labels.ConfigPath)
	}
	return defaultDir
}
