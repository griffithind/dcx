package host

import (
	"fmt"
	"path/filepath"
)

// MountConfig contains configuration for mounting the SSH agent socket.
type MountConfig struct {
	// HostPath is the path to the proxy socket directory on the host
	HostPath string

	// ContainerPath is the mount point in the container
	ContainerPath string

	// SocketName is the name of the socket file
	SocketName string

	// ContainerSocketPath is the full path to the socket in the container
	ContainerSocketPath string
}

// DefaultContainerPath is the default mount point for SSH agent in containers.
const DefaultContainerPath = "/ssh-agent"

// DefaultSocketName is the default socket filename.
const DefaultSocketName = "agent.sock"

// ProxyDirGetter is an interface for types that provide a proxy directory.
type ProxyDirGetter interface {
	ProxyDir() string
}

// NewMountConfig creates a new SSH mount configuration from a proxy.
func NewMountConfig(proxy ProxyDirGetter) *MountConfig {
	if proxy == nil {
		return nil
	}

	return &MountConfig{
		HostPath:            proxy.ProxyDir(),
		ContainerPath:       DefaultContainerPath,
		SocketName:          DefaultSocketName,
		ContainerSocketPath: filepath.Join(DefaultContainerPath, DefaultSocketName),
	}
}

// GetEnvVar returns the SSH_AUTH_SOCK environment variable value for containers.
func (m *MountConfig) GetEnvVar() string {
	if m == nil {
		return ""
	}
	return m.ContainerSocketPath
}

// GetVolumeSpec returns the Docker volume specification string.
func (m *MountConfig) GetVolumeSpec() string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s", m.HostPath, m.ContainerPath)
}

// GetVolumeSpecWithSELinux returns the volume spec with SELinux label if needed.
func (m *MountConfig) GetVolumeSpecWithSELinux(selinuxEnforcing bool) string {
	if m == nil {
		return ""
	}
	spec := m.GetVolumeSpec()
	if selinuxEnforcing {
		spec += ":Z"
	}
	return spec
}

// GetEnvironment returns the environment variables map for SSH agent.
func (m *MountConfig) GetEnvironment() map[string]string {
	if m == nil {
		return nil
	}
	return map[string]string{
		"SSH_AUTH_SOCK": m.GetEnvVar(),
	}
}
