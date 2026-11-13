package devcontainer

import (
	"strconv"
	"strings"
)

// ParseRunArgs splits docker run arguments from devcontainer.json.
//
// Flags dcx needs to reason about are extracted into structured fields:
//
//	--network/--net, --ipc, --pid, --shm-size, -u/--user,
//	--cap-drop, --device, --add-host, --sysctl
//
// Flags that would break dcx's container management (--rm deletes the
// container when it stops, --name and -d/--detach are owned by dcx) are
// collected in Ignored. Every other flag is kept verbatim in Passthrough and
// handed to `docker run`, so options like --memory, --cpus, -e, --label,
// --hostname, --ulimit, --gpus, --cap-add or -v work as written.
func ParseRunArgs(args []string) *ParsedRunArgs {
	result := &ParsedRunArgs{
		Sysctls: make(map[string]string),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			// A stray value; keep it so docker reports a clear error.
			result.Passthrough = append(result.Passthrough, arg)
			continue
		}

		flag, inline, hasInline := strings.Cut(arg, "=")
		value := func() (string, bool) {
			if hasInline {
				return inline, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}

		switch flag {
		case "--network", "--net":
			if v, ok := value(); ok {
				result.NetworkMode = v
			}
		case "--ipc":
			if v, ok := value(); ok {
				result.IpcMode = v
			}
		case "--pid":
			if v, ok := value(); ok {
				result.PidMode = v
			}
		case "--shm-size":
			if v, ok := value(); ok {
				result.ShmSize = parseShmSize(v)
			}
		case "-u", "--user":
			if v, ok := value(); ok {
				result.User = v
			}
		case "--cap-drop":
			if v, ok := value(); ok {
				result.CapDrop = append(result.CapDrop, v)
			}
		case "--device":
			if v, ok := value(); ok {
				result.Devices = append(result.Devices, v)
			}
		case "--add-host":
			if v, ok := value(); ok {
				result.ExtraHosts = append(result.ExtraHosts, v)
			}
		case "--sysctl":
			if v, ok := value(); ok {
				if k, val, ok := strings.Cut(v, "="); ok {
					result.Sysctls[k] = val
				}
			}
		case "--name":
			_, _ = value()
			result.Ignored = append(result.Ignored, arg)
		case "--rm", "-d", "--detach":
			result.Ignored = append(result.Ignored, arg)
		default:
			result.Passthrough = append(result.Passthrough, arg)
			// runArgs never contain positional arguments, so a following
			// word that is not a flag is this flag's value.
			if !hasInline && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				result.Passthrough = append(result.Passthrough, args[i])
			}
		}
	}

	return result
}

// parseShmSize parses a docker size string ("64m", "1g", "2gb", "512") into bytes.
func parseShmSize(s string) int64 {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "b") // "2gb" -> "2g"
	multiplier := int64(1)
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'k':
			multiplier, s = 1<<10, s[:n-1]
		case 'm':
			multiplier, s = 1<<20, s[:n-1]
		case 'g':
			multiplier, s = 1<<30, s[:n-1]
		}
	}
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil || val < 0 {
		return 0
	}
	return val * multiplier
}
