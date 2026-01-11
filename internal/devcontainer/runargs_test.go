package devcontainer

import (
	"testing"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected *ParsedRunArgs
	}{
		{
			name: "empty args",
			args: []string{},
			expected: &ParsedRunArgs{
				Sysctls: map[string]string{},
			},
		},
		{
			name: "network mode with equals",
			args: []string{"--network=host"},
			expected: &ParsedRunArgs{
				NetworkMode: "host",
				Sysctls:     map[string]string{},
			},
		},
		{
			name: "network mode with space",
			args: []string{"--network", "bridge"},
			expected: &ParsedRunArgs{
				NetworkMode: "bridge",
				Sysctls:     map[string]string{},
			},
		},
		{
			name: "net alias",
			args: []string{"--net=host"},
			expected: &ParsedRunArgs{
				NetworkMode: "host",
				Sysctls:     map[string]string{},
			},
		},
		{
			name: "user short flag",
			args: []string{"-u", "1000:1000"},
			expected: &ParsedRunArgs{
				User:    "1000:1000",
				Sysctls: map[string]string{},
			},
		},
		{
			name: "user long flag with equals",
			args: []string{"--user=root"},
			expected: &ParsedRunArgs{
				User:    "root",
				Sysctls: map[string]string{},
			},
		},
		{
			name: "shm-size",
			args: []string{"--shm-size=64m"},
			expected: &ParsedRunArgs{
				ShmSize: 64 * 1024 * 1024,
				Sysctls: map[string]string{},
			},
		},
		{
			name: "shm-size with space",
			args: []string{"--shm-size", "1g"},
			expected: &ParsedRunArgs{
				ShmSize: 1024 * 1024 * 1024,
				Sysctls: map[string]string{},
			},
		},
		{
			name: "ipc and pid modes",
			args: []string{"--ipc=host", "--pid", "host"},
			expected: &ParsedRunArgs{
				IpcMode: "host",
				PidMode: "host",
				Sysctls: map[string]string{},
			},
		},
		{
			name: "cap-drop multiple",
			args: []string{"--cap-drop=ALL", "--cap-drop", "NET_RAW"},
			expected: &ParsedRunArgs{
				CapDrop: []string{"ALL", "NET_RAW"},
				Sysctls: map[string]string{},
			},
		},
		{
			name: "devices",
			args: []string{"--device=/dev/snd", "--device", "/dev/dri"},
			expected: &ParsedRunArgs{
				Devices: []string{"/dev/snd", "/dev/dri"},
				Sysctls: map[string]string{},
			},
		},
		{
			name: "extra hosts",
			args: []string{"--add-host=host1:192.168.1.1", "--add-host", "host2:192.168.1.2"},
			expected: &ParsedRunArgs{
				ExtraHosts: []string{"host1:192.168.1.1", "host2:192.168.1.2"},
				Sysctls:    map[string]string{},
			},
		},
		{
			name: "sysctls",
			args: []string{"--sysctl=net.ipv4.ip_forward=1", "--sysctl", "net.core.somaxconn=1024"},
			expected: &ParsedRunArgs{
				Sysctls: map[string]string{
					"net.ipv4.ip_forward": "1",
					"net.core.somaxconn":  "1024",
				},
			},
		},
		{
			name: "mixed args",
			args: []string{
				"--network=host",
				"-u", "1000",
				"--cap-drop=ALL",
				"--device=/dev/snd",
				"--shm-size", "2g",
			},
			expected: &ParsedRunArgs{
				NetworkMode: "host",
				User:        "1000",
				CapDrop:     []string{"ALL"},
				Devices:     []string{"/dev/snd"},
				ShmSize:     2 * 1024 * 1024 * 1024,
				Sysctls:     map[string]string{},
			},
		},
		{
			name: "unknown args are ignored",
			args: []string{"--unknown=value", "--network=host", "-v", "/tmp:/tmp"},
			expected: &ParsedRunArgs{
				NetworkMode: "host",
				Sysctls:     map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseRunArgs(tt.args)

			if result.NetworkMode != tt.expected.NetworkMode {
				t.Errorf("NetworkMode = %q, want %q", result.NetworkMode, tt.expected.NetworkMode)
			}
			if result.User != tt.expected.User {
				t.Errorf("User = %q, want %q", result.User, tt.expected.User)
			}
			if result.IpcMode != tt.expected.IpcMode {
				t.Errorf("IpcMode = %q, want %q", result.IpcMode, tt.expected.IpcMode)
			}
			if result.PidMode != tt.expected.PidMode {
				t.Errorf("PidMode = %q, want %q", result.PidMode, tt.expected.PidMode)
			}
			if result.ShmSize != tt.expected.ShmSize {
				t.Errorf("ShmSize = %d, want %d", result.ShmSize, tt.expected.ShmSize)
			}
			if len(result.CapDrop) != len(tt.expected.CapDrop) {
				t.Errorf("CapDrop = %v, want %v", result.CapDrop, tt.expected.CapDrop)
			} else {
				for i, v := range result.CapDrop {
					if v != tt.expected.CapDrop[i] {
						t.Errorf("CapDrop[%d] = %q, want %q", i, v, tt.expected.CapDrop[i])
					}
				}
			}
			if len(result.Devices) != len(tt.expected.Devices) {
				t.Errorf("Devices = %v, want %v", result.Devices, tt.expected.Devices)
			} else {
				for i, v := range result.Devices {
					if v != tt.expected.Devices[i] {
						t.Errorf("Devices[%d] = %q, want %q", i, v, tt.expected.Devices[i])
					}
				}
			}
			if len(result.ExtraHosts) != len(tt.expected.ExtraHosts) {
				t.Errorf("ExtraHosts = %v, want %v", result.ExtraHosts, tt.expected.ExtraHosts)
			} else {
				for i, v := range result.ExtraHosts {
					if v != tt.expected.ExtraHosts[i] {
						t.Errorf("ExtraHosts[%d] = %q, want %q", i, v, tt.expected.ExtraHosts[i])
					}
				}
			}
			if len(result.Sysctls) != len(tt.expected.Sysctls) {
				t.Errorf("Sysctls = %v, want %v", result.Sysctls, tt.expected.Sysctls)
			} else {
				for k, v := range tt.expected.Sysctls {
					if result.Sysctls[k] != v {
						t.Errorf("Sysctls[%q] = %q, want %q", k, result.Sysctls[k], v)
					}
				}
			}
		})
	}
}

func TestParseShmSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"", 0},
		{"invalid", 0},
		{"64", 64},
		{"64b", 64},
		{"64k", 64 * 1024},
		{"64m", 64 * 1024 * 1024},
		{"2g", 2 * 1024 * 1024 * 1024},
		{"1G", 1 * 1024 * 1024 * 1024},
		{"512M", 512 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseShmSize(tt.input)
			if result != tt.expected {
				t.Errorf("parseShmSize(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}
