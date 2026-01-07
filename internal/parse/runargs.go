package parse

import "strings"

// RunArgsResult holds the parsed values from Docker runArgs.
type RunArgsResult struct {
	CapAdd      []string
	CapDrop     []string
	SecurityOpt []string
	Privileged  bool
	Init        bool
	ShmSize     string // Keep as string for flexibility
	Devices     []string
	ExtraHosts  []string
	NetworkMode string
	IpcMode     string
	PidMode     string
	Tmpfs       []string // For compose: list of paths; for docker: will be converted to map
	Sysctls     map[string]string
	Ports       []string
}

// ParseRunArgs parses Docker run-style arguments into a structured result.
func ParseRunArgs(args []string) *RunArgsResult {
	result := &RunArgsResult{
		Sysctls: make(map[string]string),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		// Cap add
		case strings.HasPrefix(arg, "--cap-add="):
			result.CapAdd = append(result.CapAdd, strings.TrimPrefix(arg, "--cap-add="))
		case arg == "--cap-add" && i+1 < len(args):
			i++
			result.CapAdd = append(result.CapAdd, args[i])

		// Cap drop
		case strings.HasPrefix(arg, "--cap-drop="):
			result.CapDrop = append(result.CapDrop, strings.TrimPrefix(arg, "--cap-drop="))
		case arg == "--cap-drop" && i+1 < len(args):
			i++
			result.CapDrop = append(result.CapDrop, args[i])

		// Security options
		case strings.HasPrefix(arg, "--security-opt="):
			result.SecurityOpt = append(result.SecurityOpt, strings.TrimPrefix(arg, "--security-opt="))
		case arg == "--security-opt" && i+1 < len(args):
			i++
			result.SecurityOpt = append(result.SecurityOpt, args[i])

		// Privileged
		case arg == "--privileged":
			result.Privileged = true

		// Init
		case arg == "--init":
			result.Init = true

		// Shared memory size
		case strings.HasPrefix(arg, "--shm-size="):
			result.ShmSize = strings.TrimPrefix(arg, "--shm-size=")
		case arg == "--shm-size" && i+1 < len(args):
			i++
			result.ShmSize = args[i]

		// Devices
		case strings.HasPrefix(arg, "--device="):
			result.Devices = append(result.Devices, strings.TrimPrefix(arg, "--device="))
		case arg == "--device" && i+1 < len(args):
			i++
			result.Devices = append(result.Devices, args[i])

		// Extra hosts
		case strings.HasPrefix(arg, "--add-host="):
			result.ExtraHosts = append(result.ExtraHosts, strings.TrimPrefix(arg, "--add-host="))
		case arg == "--add-host" && i+1 < len(args):
			i++
			result.ExtraHosts = append(result.ExtraHosts, args[i])

		// Network mode
		case strings.HasPrefix(arg, "--network="):
			result.NetworkMode = strings.TrimPrefix(arg, "--network=")
		case strings.HasPrefix(arg, "--net="):
			result.NetworkMode = strings.TrimPrefix(arg, "--net=")
		case arg == "--network" && i+1 < len(args):
			i++
			result.NetworkMode = args[i]
		case arg == "--net" && i+1 < len(args):
			i++
			result.NetworkMode = args[i]

		// IPC mode
		case strings.HasPrefix(arg, "--ipc="):
			result.IpcMode = strings.TrimPrefix(arg, "--ipc=")
		case arg == "--ipc" && i+1 < len(args):
			i++
			result.IpcMode = args[i]

		// PID mode
		case strings.HasPrefix(arg, "--pid="):
			result.PidMode = strings.TrimPrefix(arg, "--pid=")
		case arg == "--pid" && i+1 < len(args):
			i++
			result.PidMode = args[i]

		// Tmpfs
		case strings.HasPrefix(arg, "--tmpfs="):
			result.Tmpfs = append(result.Tmpfs, strings.TrimPrefix(arg, "--tmpfs="))
		case arg == "--tmpfs" && i+1 < len(args):
			i++
			result.Tmpfs = append(result.Tmpfs, args[i])

		// Sysctl
		case strings.HasPrefix(arg, "--sysctl="):
			ParseSysctl(result.Sysctls, strings.TrimPrefix(arg, "--sysctl="))
		case arg == "--sysctl" && i+1 < len(args):
			i++
			ParseSysctl(result.Sysctls, args[i])

		// Publish ports
		case strings.HasPrefix(arg, "-p="):
			result.Ports = append(result.Ports, strings.TrimPrefix(arg, "-p="))
		case arg == "-p" && i+1 < len(args):
			i++
			result.Ports = append(result.Ports, args[i])
		case strings.HasPrefix(arg, "--publish="):
			result.Ports = append(result.Ports, strings.TrimPrefix(arg, "--publish="))
		case arg == "--publish" && i+1 < len(args):
			i++
			result.Ports = append(result.Ports, args[i])
		}
	}

	return result
}

// TmpfsAsMap converts the Tmpfs list to a map format used by Docker API.
// Format: "/path" or "/path:options"
func (r *RunArgsResult) TmpfsAsMap() map[string]string {
	result := make(map[string]string)
	for _, spec := range r.Tmpfs {
		ParseTmpfs(result, spec)
	}
	return result
}
