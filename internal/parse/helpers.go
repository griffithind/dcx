package parse

import "strings"

// ParseSysctl parses a sysctl key=value pair into the provided map.
func ParseSysctl(sysctls map[string]string, value string) {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) == 2 {
		sysctls[parts[0]] = parts[1]
	}
}

// ParseTmpfs parses a tmpfs mount specification into the provided map.
// Format: "/path" or "/path:options"
func ParseTmpfs(tmpfs map[string]string, value string) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 1 {
		tmpfs[parts[0]] = ""
	} else {
		tmpfs[parts[0]] = parts[1]
	}
}

// ParseShmSize parses a shared memory size string (e.g., "1g", "512m", "1024").
// Returns the size in bytes.
func ParseShmSize(size string) int64 {
	if size == "" {
		return 0
	}

	size = strings.TrimSpace(size)
	if len(size) == 0 {
		return 0
	}

	var multiplier int64 = 1
	lastChar := size[len(size)-1]

	switch lastChar {
	case 'k', 'K':
		multiplier = 1024
		size = size[:len(size)-1]
	case 'm', 'M':
		multiplier = 1024 * 1024
		size = size[:len(size)-1]
	case 'g', 'G':
		multiplier = 1024 * 1024 * 1024
		size = size[:len(size)-1]
	case 'b', 'B':
		size = size[:len(size)-1]
	}

	var num int64
	for _, c := range size {
		if c >= '0' && c <= '9' {
			num = num*10 + int64(c-'0')
		}
	}

	return num * multiplier
}
