package build

import (
	"testing"
)

func TestShouldUpdateRemoteUserUID(t *testing.T) {
	tests := []struct {
		name       string
		remoteUser string
		hostUID    int
		expected   bool
	}{
		{
			name:       "normal user",
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   true,
		},
		{
			name:       "root user",
			remoteUser: "root",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "root UID string",
			remoteUser: "0",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "host is root",
			remoteUser: "vscode",
			hostUID:    0,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUID(tt.remoteUser, tt.hostUID)
			if result != tt.expected {
				t.Errorf("ShouldUpdateRemoteUserUID(%q, %d) = %v, expected %v",
					tt.remoteUser, tt.hostUID, result, tt.expected)
			}
		})
	}
}

func TestShouldUpdateRemoteUserUIDWithConfig(t *testing.T) {
	boolTrue := true
	boolFalse := false

	tests := []struct {
		name                string
		updateRemoteUserUID *bool
		remoteUser          string
		hostUID             int
		expected            bool
	}{
		{
			name:                "explicitly enabled",
			updateRemoteUserUID: &boolTrue,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            true,
		},
		{
			name:                "explicitly disabled",
			updateRemoteUserUID: &boolFalse,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            false,
		},
		{
			name:                "default (nil) - should update",
			updateRemoteUserUID: nil,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            true,
		},
		{
			name:                "root user - config ignored",
			updateRemoteUserUID: &boolTrue,
			remoteUser:          "root",
			hostUID:             1000,
			expected:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUIDWithConfig(tt.updateRemoteUserUID, tt.remoteUser, tt.hostUID)
			if result != tt.expected {
				t.Errorf("ShouldUpdateRemoteUserUIDWithConfig = %v, expected %v", result, tt.expected)
			}
		})
	}
}
