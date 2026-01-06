package compose

import (
	"testing"

	"github.com/griffithind/dcx/internal/config"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestMapRunArgsToService(t *testing.T) {
	tests := []struct {
		name     string
		runArgs  []string
		expected ServiceOverride
	}{
		{
			name:    "cap-add with equals",
			runArgs: []string{"--cap-add=SYS_PTRACE"},
			expected: ServiceOverride{
				CapAdd: []string{"SYS_PTRACE"},
			},
		},
		{
			name:    "cap-add with space",
			runArgs: []string{"--cap-add", "SYS_PTRACE"},
			expected: ServiceOverride{
				CapAdd: []string{"SYS_PTRACE"},
			},
		},
		{
			name:    "cap-drop with equals",
			runArgs: []string{"--cap-drop=NET_RAW"},
			expected: ServiceOverride{
				CapDrop: []string{"NET_RAW"},
			},
		},
		{
			name:    "privileged",
			runArgs: []string{"--privileged"},
			expected: ServiceOverride{
				Privileged: boolPtr(true),
			},
		},
		{
			name:    "init",
			runArgs: []string{"--init"},
			expected: ServiceOverride{
				Init: boolPtr(true),
			},
		},
		{
			name:    "shm-size with equals",
			runArgs: []string{"--shm-size=2g"},
			expected: ServiceOverride{
				ShmSize: "2g",
			},
		},
		{
			name:    "device with equals",
			runArgs: []string{"--device=/dev/fuse"},
			expected: ServiceOverride{
				Devices: []string{"/dev/fuse"},
			},
		},
		{
			name:    "add-host with equals",
			runArgs: []string{"--add-host=host.docker.internal:host-gateway"},
			expected: ServiceOverride{
				ExtraHosts: []string{"host.docker.internal:host-gateway"},
			},
		},
		{
			name:    "network mode with equals",
			runArgs: []string{"--network=host"},
			expected: ServiceOverride{
				NetworkMode: "host",
			},
		},
		{
			name:    "network mode short form",
			runArgs: []string{"--net=bridge"},
			expected: ServiceOverride{
				NetworkMode: "bridge",
			},
		},
		{
			name:    "ipc mode",
			runArgs: []string{"--ipc=host"},
			expected: ServiceOverride{
				IpcMode: "host",
			},
		},
		{
			name:    "pid mode",
			runArgs: []string{"--pid=host"},
			expected: ServiceOverride{
				PidMode: "host",
			},
		},
		{
			name:    "tmpfs with equals",
			runArgs: []string{"--tmpfs=/run"},
			expected: ServiceOverride{
				Tmpfs: []string{"/run"},
			},
		},
		{
			name:    "sysctl with equals",
			runArgs: []string{"--sysctl=net.core.somaxconn=1024"},
			expected: ServiceOverride{
				Sysctls: map[string]string{"net.core.somaxconn": "1024"},
			},
		},
		{
			name:    "port with -p",
			runArgs: []string{"-p", "8080:80"},
			expected: ServiceOverride{
				Ports: []string{"8080:80"},
			},
		},
		{
			name:    "port with --publish",
			runArgs: []string{"--publish=3000:3000"},
			expected: ServiceOverride{
				Ports: []string{"3000:3000"},
			},
		},
		{
			name:    "multiple options",
			runArgs: []string{"--privileged", "--network=host", "--ipc=host", "-p", "8080:80"},
			expected: ServiceOverride{
				Privileged:  boolPtr(true),
				NetworkMode: "host",
				IpcMode:     "host",
				Ports:       []string{"8080:80"},
			},
		},
		{
			name:    "security-opt with equals",
			runArgs: []string{"--security-opt=seccomp=unconfined"},
			expected: ServiceOverride{
				SecurityOpt: []string{"seccomp=unconfined"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.DevcontainerConfig{
				RunArgs: tt.runArgs,
				Service: "test",
			}
			gen := &overrideGenerator{cfg: cfg}
			svc := &ServiceOverride{}
			gen.mapRunArgsToService(svc)

			// Compare relevant fields
			if tt.expected.CapAdd != nil {
				assert.Equal(t, tt.expected.CapAdd, svc.CapAdd)
			}
			if tt.expected.CapDrop != nil {
				assert.Equal(t, tt.expected.CapDrop, svc.CapDrop)
			}
			if tt.expected.Privileged != nil {
				assert.Equal(t, tt.expected.Privileged, svc.Privileged)
			}
			if tt.expected.Init != nil {
				assert.Equal(t, tt.expected.Init, svc.Init)
			}
			if tt.expected.ShmSize != "" {
				assert.Equal(t, tt.expected.ShmSize, svc.ShmSize)
			}
			if tt.expected.Devices != nil {
				assert.Equal(t, tt.expected.Devices, svc.Devices)
			}
			if tt.expected.ExtraHosts != nil {
				assert.Equal(t, tt.expected.ExtraHosts, svc.ExtraHosts)
			}
			if tt.expected.NetworkMode != "" {
				assert.Equal(t, tt.expected.NetworkMode, svc.NetworkMode)
			}
			if tt.expected.IpcMode != "" {
				assert.Equal(t, tt.expected.IpcMode, svc.IpcMode)
			}
			if tt.expected.PidMode != "" {
				assert.Equal(t, tt.expected.PidMode, svc.PidMode)
			}
			if tt.expected.Tmpfs != nil {
				assert.Equal(t, tt.expected.Tmpfs, svc.Tmpfs)
			}
			if tt.expected.Sysctls != nil {
				assert.Equal(t, tt.expected.Sysctls, svc.Sysctls)
			}
			if tt.expected.Ports != nil {
				assert.Equal(t, tt.expected.Ports, svc.Ports)
			}
			if tt.expected.SecurityOpt != nil {
				assert.Equal(t, tt.expected.SecurityOpt, svc.SecurityOpt)
			}
		})
	}
}

func TestParseMountString(t *testing.T) {
	tests := []struct {
		name     string
		mount    string
		expected string
	}{
		{
			name:     "bind mount",
			mount:    "source=/host/path,target=/container/path,type=bind",
			expected: "/host/path:/container/path",
		},
		{
			name:     "bind mount with src/dst",
			mount:    "src=/host/path,dst=/container/path,type=bind",
			expected: "/host/path:/container/path",
		},
		{
			name:     "volume mount",
			mount:    "source=myvolume,target=/data,type=volume",
			expected: "myvolume:/data",
		},
		{
			name:     "tmpfs mount",
			mount:    "target=/tmp,type=tmpfs",
			expected: "tmpfs:/tmp",
		},
		{
			name:     "default type is bind",
			mount:    "source=/host,target=/container",
			expected: "/host:/container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := &overrideGenerator{}
			result := gen.parseMountString(tt.mount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateOverrideWithForwardPorts(t *testing.T) {
	cfg := &config.DevcontainerConfig{
		Service:      "app",
		ForwardPorts: []interface{}{float64(8080), float64(3000)},
	}

	gen := &overrideGenerator{
		cfg:           cfg,
		envKey:        "testenv",
		configHash:    "abc123",
		workspacePath: "/workspace",
	}

	content, err := gen.Generate()
	assert.NoError(t, err)

	// Parse the YAML to verify
	var override ComposeOverride
	err = yaml.Unmarshal([]byte(content), &override)
	assert.NoError(t, err)

	svc := override.Services["app"]
	assert.Contains(t, svc.Ports, "8080:8080")
	assert.Contains(t, svc.Ports, "3000:3000")
}

func boolPtr(b bool) *bool {
	return &b
}
