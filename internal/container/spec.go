package container

import (
	"sort"

	"github.com/griffithind/dcx/internal/devcontainer"
)

// ContainerSpec holds every runtime setting of the primary dev container.
// It is built once from the resolved configuration (UnifiedRuntime.spec) and
// rendered by both engines: dockerRunArgs for single-container plans and
// composeOverride for compose plans. Settings therefore cannot apply to one
// plan type and silently not to the other; TestContainerSpecParity fails
// when a field is added without being rendered by both.
type ContainerSpec struct {
	// Name is the container name. Compose names its own containers, so the
	// compose rendering ignores it.
	Name string
	// Image to run. For compose, empty means "keep the service's image".
	Image string
	User  string
	// WorkingDir is the container's working directory. Compose plans keep
	// the service's own working_dir (the service command may depend on it),
	// so the spec builder leaves this empty for them.
	WorkingDir string

	Labels map[string]string
	Env    map[string]string

	// Mounts includes the workspace mount for single-container plans.
	Mounts []devcontainer.Mount
	// Tmpfs maps a container path to its mount options ("" for defaults).
	Tmpfs map[string]string
	Ports []devcontainer.PortForward

	CapAdd      []string
	CapDrop     []string
	SecurityOpt []string
	Privileged  bool
	Init        bool

	NetworkMode string
	IpcMode     string
	PidMode     string
	ShmSize     int64
	Devices     []string
	ExtraHosts  []string
	Sysctls     map[string]string

	// GPUs is "" (none), "all", or a device count such as "2".
	GPUs string

	// Entrypoint replaces the image entrypoint when set; its first element
	// is the executable. Cmd replaces the image command.
	Entrypoint []string
	Cmd        []string

	// RunArgs are devcontainer.json runArgs dcx does not model, passed to
	// `docker run` verbatim. They have no compose equivalent (users are
	// warned at resolution time).
	RunArgs []string
}

// keepAliveEntrypoint keeps the container running when overrideCommand is
// in effect. Anything appended by the image or compose `command` becomes a
// positional parameter of the script and is ignored, so the override works
// no matter what command the service defines. The trap makes `docker stop`
// return immediately instead of waiting for the kill timeout, since a
// shell running as PID 1 ignores SIGTERM without a handler.
var keepAliveEntrypoint = []string{
	"/bin/sh", "-c",
	`trap "exit 0" TERM INT; sleep infinity & wait`,
	"dcx-keepalive",
}

// sortedKeys returns the keys of m in sorted order, so renderings are
// deterministic.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
