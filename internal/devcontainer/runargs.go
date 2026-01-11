package devcontainer

import (
	"strconv"
	"strings"
)

// ParseRunArgs parses docker run arguments from devcontainer.json and extracts known flags.
// This allows runArgs to be applied to container creation in a structured way.
//
// Supported flags:
//   - --network, --net: Network mode
//   - --ipc: IPC mode
//   - --pid: PID mode
//   - --shm-size: Shared memory size
//   - -u, --user: Container user
//   - --cap-drop: Capabilities to drop
//   - --device: Devices to add
//   - --add-host: Extra hosts
//   - --sysctl: Sysctl settings
func ParseRunArgs(args []string) *ParsedRunArgs {
	result := &ParsedRunArgs{
		Sysctls: make(map[string]string),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle --flag=value syntax
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			flag := parts[0]
			value := parts[1]

			switch flag {
			case "--network", "--net":
				result.NetworkMode = value
			case "--ipc":
				result.IpcMode = value
			case "--pid":
				result.PidMode = value
			case "--shm-size":
				result.ShmSize = parseShmSize(value)
			case "-u", "--user":
				result.User = value
			case "--cap-drop":
				result.CapDrop = append(result.CapDrop, value)
			case "--device":
				result.Devices = append(result.Devices, value)
			case "--add-host":
				result.ExtraHosts = append(result.ExtraHosts, value)
			case "--sysctl":
				if kv := strings.SplitN(value, "=", 2); len(kv) == 2 {
					result.Sysctls[kv[0]] = kv[1]
				}
			}
			continue
		}

		// Handle --flag value syntax (requires next arg)
		if i+1 >= len(args) {
			continue
		}
		value := args[i+1]

		switch arg {
		case "--network", "--net":
			result.NetworkMode = value
			i++
		case "--ipc":
			result.IpcMode = value
			i++
		case "--pid":
			result.PidMode = value
			i++
		case "--shm-size":
			result.ShmSize = parseShmSize(value)
			i++
		case "-u", "--user":
			result.User = value
			i++
		case "--cap-drop":
			result.CapDrop = append(result.CapDrop, value)
			i++
		case "--device":
			result.Devices = append(result.Devices, value)
			i++
		case "--add-host":
			result.ExtraHosts = append(result.ExtraHosts, value)
			i++
		case "--sysctl":
			if kv := strings.SplitN(value, "=", 2); len(kv) == 2 {
				result.Sysctls[kv[0]] = kv[1]
			}
			i++
		}
	}

	return result
}

// parseShmSize parses a size string (e.g., "64m", "1g") into bytes.
func parseShmSize(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "b") {
		s = s[:len(s)-1]
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return val * multiplier
}
