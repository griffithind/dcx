package features

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
		name       string
		cfg        *config.DevcontainerConfig
		remoteUser string
		hostUID    int
		expected   bool
	}{
		{
			name:       "explicitly true on supported platform",
			cfg:        &config.DevcontainerConfig{UpdateRemoteUserUID: &trueVal},
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   runtime.GOOS != "windows",
		},
		{
			name:       "explicitly false",
			cfg:        &config.DevcontainerConfig{UpdateRemoteUserUID: &falseVal},
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "not set (nil) - defaults to true on Linux/macOS",
			cfg:        &config.DevcontainerConfig{UpdateRemoteUserUID: nil},
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   runtime.GOOS != "windows",
		},
		{
			name:       "empty config with user",
			cfg:        &config.DevcontainerConfig{},
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   runtime.GOOS != "windows",
		},
		{
			name:       "skip root user",
			cfg:        &config.DevcontainerConfig{},
			remoteUser: "root",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "skip root user (numeric)",
			cfg:        &config.DevcontainerConfig{},
			remoteUser: "0",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "skip when host is root",
			cfg:        &config.DevcontainerConfig{},
			remoteUser: "vscode",
			hostUID:    0,
			expected:   false,
		},
		{
			name:       "nil config",
			cfg:        nil,
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   runtime.GOOS != "windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUID(tt.cfg, tt.remoteUser, tt.hostUID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldUpdateRemoteUserUID_PlatformSpecific(t *testing.T) {
	cfg := &config.DevcontainerConfig{}

	switch runtime.GOOS {
	case "linux":
		// Linux should update by default
		assert.True(t, ShouldUpdateRemoteUserUID(cfg, "vscode", 1000))
	case "darwin":
		// macOS should update by default (for virtiofs)
		assert.True(t, ShouldUpdateRemoteUserUID(cfg, "vscode", 501))
	case "windows":
		// Windows should not update (different file sharing semantics)
		assert.False(t, ShouldUpdateRemoteUserUID(cfg, "vscode", 1000))
	}
}
