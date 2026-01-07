package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRunArgs(t *testing.T) {
	args := []string{
		"--cap-add=SYS_PTRACE",
		"--cap-add", "NET_ADMIN",
		"--cap-drop=ALL",
		"--security-opt=seccomp=unconfined",
		"--privileged",
		"--init",
		"--shm-size=1g",
		"--device=/dev/fuse",
		"--add-host=host.docker.internal:host-gateway",
		"--network=host",
		"--ipc=host",
		"--pid=host",
		"--tmpfs=/run:size=100m",
		"--sysctl=net.core.somaxconn=1024",
		"-p", "8080:80",
		"--publish=3000:3000",
	}

	result := ParseRunArgs(args)

	assert.Equal(t, []string{"SYS_PTRACE", "NET_ADMIN"}, result.CapAdd)
	assert.Equal(t, []string{"ALL"}, result.CapDrop)
	assert.Equal(t, []string{"seccomp=unconfined"}, result.SecurityOpt)
	assert.True(t, result.Privileged)
	assert.True(t, result.Init)
	assert.Equal(t, "1g", result.ShmSize)
	assert.Equal(t, []string{"/dev/fuse"}, result.Devices)
	assert.Equal(t, []string{"host.docker.internal:host-gateway"}, result.ExtraHosts)
	assert.Equal(t, "host", result.NetworkMode)
	assert.Equal(t, "host", result.IpcMode)
	assert.Equal(t, "host", result.PidMode)
	assert.Equal(t, []string{"/run:size=100m"}, result.Tmpfs)
	assert.Equal(t, map[string]string{"net.core.somaxconn": "1024"}, result.Sysctls)
	assert.Equal(t, []string{"8080:80", "3000:3000"}, result.Ports)
}

func TestParseRunArgs_Empty(t *testing.T) {
	result := ParseRunArgs(nil)
	assert.NotNil(t, result)
	assert.Empty(t, result.CapAdd)
	assert.False(t, result.Privileged)
}

func TestParseRunArgs_NetworkAliases(t *testing.T) {
	// Test --net= alias
	result := ParseRunArgs([]string{"--net=bridge"})
	assert.Equal(t, "bridge", result.NetworkMode)

	// Test --network with space
	result = ParseRunArgs([]string{"--network", "custom"})
	assert.Equal(t, "custom", result.NetworkMode)
}

func TestRunArgsResult_TmpfsAsMap(t *testing.T) {
	result := &RunArgsResult{
		Tmpfs: []string{
			"/run",
			"/tmp:size=100m,mode=1777",
		},
	}

	tmpfsMap := result.TmpfsAsMap()
	assert.Equal(t, "", tmpfsMap["/run"])
	assert.Equal(t, "size=100m,mode=1777", tmpfsMap["/tmp"])
}
