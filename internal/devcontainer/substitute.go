package devcontainer

import (
	"os"
	"regexp"
	"strings"
)

// SubstitutionContext provides values for variable substitution (see
// Substitute).
type SubstitutionContext struct {
	LocalWorkspaceFolder     string
	ContainerWorkspaceFolder string
	DevcontainerID           string
	UserHome                 string // ${userHome}
	// LookupEnv resolves ${localEnv:VAR}; nil resolves nothing.
	LookupEnv func(string) (string, bool)
}

// NewSubstitutionContext builds the substitution context for a workspace.
// It is the single place the context is assembled, so every caller sees the
// same values for ${devcontainerId}, ${userHome}, the workspace folders, etc.
//
// ${containerEnv:VAR} is intentionally left unresolved here: container
// environment only exists once the container runs, so those references are
// resolved at exec time (see ResolveContainerEnvRefs).
func NewSubstitutionContext(workspaceRoot, configPath string, cfg *DevContainerConfig) *SubstitutionContext {
	ctx := &SubstitutionContext{
		LocalWorkspaceFolder: workspaceRoot,
		DevcontainerID:       WorkspaceID(workspaceRoot, configPath),
		LookupEnv:            os.LookupEnv,
	}
	if home, err := os.UserHomeDir(); err == nil {
		ctx.UserHome = home
	}
	if cfg != nil {
		// workspaceFolder may itself reference local variables
		// (e.g. /src/${localWorkspaceFolderBasename}); resolve it before it
		// becomes ${containerWorkspaceFolder}.
		folder := *cfg
		folder.WorkspaceFolder = Substitute(cfg.WorkspaceFolder, ctx)
		ctx.ContainerWorkspaceFolder = DetermineContainerWorkspaceFolder(&folder, workspaceRoot)
	}
	return ctx
}

var containerEnvRef = regexp.MustCompile(`\$\{containerEnv:([^}:]+)(?::([^}]*))?\}`)

// ResolveContainerEnvRefs resolves ${containerEnv:VAR[:default]} references
// in values (typically remoteEnv) against the running container's
// environment; the default applies when VAR is unset. Other text is left
// unchanged.
func ResolveContainerEnvRefs(values map[string]string, containerEnv map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = containerEnvRef.ReplaceAllStringFunc(v, func(match string) string {
			parts := containerEnvRef.FindStringSubmatch(match)
			if val, ok := containerEnv[parts[1]]; ok {
				return val
			}
			return parts[2]
		})
	}
	return out
}

// EnvListToMap converts KEY=VALUE pairs (as reported by docker inspect) to a map.
func EnvListToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}
