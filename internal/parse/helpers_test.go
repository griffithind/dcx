package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSysctl(t *testing.T) {
	sysctls := make(map[string]string)

	ParseSysctl(sysctls, "net.core.somaxconn=1024")
	assert.Equal(t, "1024", sysctls["net.core.somaxconn"])

	ParseSysctl(sysctls, "kernel.shmmax=68719476736")
	assert.Equal(t, "68719476736", sysctls["kernel.shmmax"])

	// Invalid format (no =)
	ParseSysctl(sysctls, "invalid")
	assert.Len(t, sysctls, 2)
}

func TestParseTmpfs(t *testing.T) {
	tmpfs := make(map[string]string)

	ParseTmpfs(tmpfs, "/run")
	assert.Equal(t, "", tmpfs["/run"])

	ParseTmpfs(tmpfs, "/tmp:size=100m,mode=1777")
	assert.Equal(t, "size=100m,mode=1777", tmpfs["/tmp"])
}
