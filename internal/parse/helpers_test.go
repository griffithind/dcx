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

func TestParseShmSize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"empty", "", 0},
		{"bytes", "1024", 1024},
		{"kilobytes", "1k", 1024},
		{"kilobytes upper", "1K", 1024},
		{"megabytes", "64m", 64 * 1024 * 1024},
		{"megabytes upper", "64M", 64 * 1024 * 1024},
		{"gigabytes", "1g", 1024 * 1024 * 1024},
		{"gigabytes upper", "1G", 1024 * 1024 * 1024},
		{"with b suffix", "1024b", 1024},
		{"with spaces", "  512m  ", 512 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseShmSize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
