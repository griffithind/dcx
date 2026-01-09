package features

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldUpdateRemoteUserUID(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name               string
		updateRemoteUserUID *bool
		remoteUser         string
		hostUID            int
		expected           bool
	}{
		{
			name:               "explicitly true on supported platform",
			updateRemoteUserUID: &trueVal,
			remoteUser:         "vscode",
			hostUID:            1000,
			expected:           runtime.GOOS != "windows",
		},
		{
			name:               "explicitly false",
			updateRemoteUserUID: &falseVal,
			remoteUser:         "vscode",
			hostUID:            1000,
			expected:           false,
		},
		{
			name:               "not set (nil) - defaults to true on Linux/macOS",
			updateRemoteUserUID: nil,
			remoteUser:         "vscode",
			hostUID:            1000,
			expected:           runtime.GOOS != "windows",
		},
		{
			name:               "skip root user",
			updateRemoteUserUID: nil,
			remoteUser:         "root",
			hostUID:            1000,
			expected:           false,
		},
		{
			name:               "skip root user (numeric)",
			updateRemoteUserUID: nil,
			remoteUser:         "0",
			hostUID:            1000,
			expected:           false,
		},
		{
			name:               "skip when host is root",
			updateRemoteUserUID: nil,
			remoteUser:         "vscode",
			hostUID:            0,
			expected:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUID(tt.updateRemoteUserUID, tt.remoteUser, tt.hostUID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldUpdateRemoteUserUID_PlatformSpecific(t *testing.T) {
	switch runtime.GOOS {
	case "linux":
		// Linux should update by default
		assert.True(t, ShouldUpdateRemoteUserUID(nil, "vscode", 1000))
	case "darwin":
		// macOS should update by default (for virtiofs)
		assert.True(t, ShouldUpdateRemoteUserUID(nil, "vscode", 501))
	case "windows":
		// Windows should not update (different file sharing semantics)
		assert.False(t, ShouldUpdateRemoteUserUID(nil, "vscode", 1000))
	}
}
