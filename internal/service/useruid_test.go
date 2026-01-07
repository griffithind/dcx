package service

import (
	"runtime"
	"testing"

	"github.com/griffithind/dcx/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestShouldUpdateRemoteUserUID(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		cfg      *config.DevcontainerConfig
		expected bool
	}{
		{
			name:     "explicitly true",
			cfg:      &config.DevcontainerConfig{UpdateRemoteUserUID: &trueVal},
			expected: runtime.GOOS == "linux", // Only true on Linux
		},
		{
			name:     "explicitly false",
			cfg:      &config.DevcontainerConfig{UpdateRemoteUserUID: &falseVal},
			expected: false,
		},
		{
			name:     "not set (nil) - defaults to true on Linux",
			cfg:      &config.DevcontainerConfig{UpdateRemoteUserUID: nil},
			expected: runtime.GOOS == "linux", // Default true on Linux only
		},
		{
			name:     "empty config",
			cfg:      &config.DevcontainerConfig{},
			expected: runtime.GOOS == "linux",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldUpdateRemoteUserUID(tt.cfg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetRemoteUser(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.DevcontainerConfig
		workspacePath string
		expected      string
	}{
		{
			name:          "remoteUser set",
			cfg:           &config.DevcontainerConfig{RemoteUser: "vscode"},
			workspacePath: "/home/user/project",
			expected:      "vscode",
		},
		{
			name:          "containerUser fallback",
			cfg:           &config.DevcontainerConfig{ContainerUser: "node"},
			workspacePath: "/home/user/project",
			expected:      "node",
		},
		{
			name:          "remoteUser takes precedence",
			cfg:           &config.DevcontainerConfig{RemoteUser: "vscode", ContainerUser: "node"},
			workspacePath: "/home/user/project",
			expected:      "vscode",
		},
		{
			name:          "no user set",
			cfg:           &config.DevcontainerConfig{},
			workspacePath: "/home/user/project",
			expected:      "",
		},
		{
			name:          "with variable substitution",
			cfg:           &config.DevcontainerConfig{RemoteUser: "${localWorkspaceFolderBasename}"},
			workspacePath: "/home/user/myproject",
			expected:      "myproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getRemoteUser(tt.cfg, tt.workspacePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRootUser(t *testing.T) {
	tests := []struct {
		username string
		expected bool
	}{
		{"root", true},
		{"0", true},
		{"vscode", false},
		{"node", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			assert.Equal(t, tt.expected, isRootUser(tt.username))
		})
	}
}
