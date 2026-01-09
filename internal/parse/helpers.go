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
