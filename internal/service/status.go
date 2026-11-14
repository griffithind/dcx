package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
)

// StatusReport is what `dcx status` shows. The CLI only renders it.
type StatusReport struct {
	WorkspacePath string
	Identity      *devcontainer.Identity
	State         state.ContainerState
	// Container is the primary container, nil when absent.
	Container *state.ContainerInfo
	// SSHAlias is the alias `ssh` reaches the workspace by, "" when no SSH
	// config block exists.
	SSHAlias string
	// Config is the devcontainer.json (after image metadata when it could
	// be resolved), nil when it could not be read.
	Config *devcontainer.DevContainerConfig
	// ConfigHash is the current staleness key, "" when the config could not
	// be fully resolved (then staleness is not judged).
	ConfigHash string
	// ConfigError says why the config could not be resolved ("" when it
	// was).
	ConfigError string
	Shortcuts   int

	// Drift reports that inputs changed while the files did not (see
	// inputsDrifted): the container is current but not what an up from
	// scratch would create.
	Drift bool

	// From the resolved config, when it could be resolved.
	PlanType  devcontainer.PlanType
	BaseImage string
	Features  []string // resolved, in install order

	// SSH is how the agent is reached, nil while there is no container.
	SSH *SSHStatus

	// Filled in only when detailed output was requested.
	Image     string
	StartedAt string
}

// SSHStatus is the workspace's SSH endpoint as recorded at create time.
type SSHStatus struct {
	// Port is the host port the agent listens on (0 when unknown).
	Port int
	// BindAddress is the host address the port is published on.
	BindAddress string
	// AllowedIPs are the source ranges the agent accepts (loopback, plus
	// --hosts).
	AllowedIPs []string
	// HostKeyFingerprint and HostKeyFile describe the workspace's host
	// key ("" before the first up).
	HostKeyFingerprint string
	HostKeyFile        string
	// KnownHostsPinned reports whether known_hosts pins the host key.
	KnownHostsPinned bool
}

// Status reports the workspace's state. It resolves the config the same
// way `dcx up` does, so its staleness verdict matches what up would do;
// when that fails (offline features, invalid JSON) it falls back to the
// raw file, and then to container state alone, rather than guessing.
func (s *DevContainerService) Status(ctx context.Context, detailed bool) (*StatusReport, error) {
	ids := s.Identity()
	r := &StatusReport{WorkspacePath: s.workspacePath, Identity: ids}

	var err error
	if resolved, rerr := s.Load(ctx); rerr == nil {
		r.Config, r.ConfigHash = resolved.Config, resolved.ConfigHash
		r.PlanType, r.BaseImage = resolved.Plan.Type(), resolved.BaseImage
		for _, f := range resolved.Features {
			r.Features = append(r.Features, f.ID)
		}
		r.State, r.Container, err = s.stateManager.StateWithHash(ctx, ids.WorkspaceID, resolved.ConfigHash)
		r.Drift = err == nil && r.State != state.StateStale && inputsDrifted(r.Container, resolved)
	} else {
		r.ConfigError = rerr.Error()
		if cfg, _, lerr := devcontainer.Load(s.workspacePath, s.configPath); lerr == nil {
			r.Config = cfg
		}
		r.State, r.Container, err = s.stateManager.State(ctx, ids.WorkspaceID)
	}
	if err != nil {
		// Without a config there is nothing to report on: say that rather
		// than leading with why Docker couldn't be asked.
		if r.Config == nil && r.ConfigError != "" {
			return nil, fmt.Errorf("%s (and the container state is unavailable: %w)", r.ConfigError, err)
		}
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	if r.Config != nil {
		if custom := r.Config.DcxCustomizations(); custom != nil {
			r.Shortcuts = len(custom.Shortcuts)
		}
	}
	if r.Container != nil {
		if aliases := hostconfig.Aliases(r.Container.Name); len(aliases) > 0 {
			r.SSHAlias = aliases[0]
		}
		r.SSH = s.sshStatus(ctx, ids.WorkspaceID, r.Container)
		if detailed {
			if details, ierr := s.engine.InspectContainer(ctx, r.Container.ID); ierr == nil && details != nil {
				r.Image, r.StartedAt = details.Image, details.StartedAt
			}
		}
	}
	return r, nil
}

// Environment is one dcx-managed workspace on this Docker host.
type Environment struct {
	WorkspaceID   string
	Name          string
	WorkspacePath string
	State         state.ContainerState
	BuildMethod   string
	Containers    []EnvironmentContainer
	CreatedAt     time.Time
}

// EnvironmentContainer is one container of an Environment.
type EnvironmentContainer struct {
	ID        string
	Name      string
	Status    string
	IsPrimary bool
	CreatedAt time.Time
}

// List returns every dcx-managed environment on the host, sorted by
// workspace path. Without all, environments with no running container are
// left out.
func (s *DevContainerService) List(ctx context.Context, all bool) ([]Environment, error) {
	containers, err := s.engine.ListContainersWithLabels(ctx, map[string]string{state.LabelManaged: "true"})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	byID := map[string]*Environment{}
	for _, c := range containers {
		if !all && !c.Running {
			continue
		}
		labels := state.ContainerLabelsFromMap(c.Labels)
		if labels.WorkspaceID == "" {
			continue
		}
		env, ok := byID[labels.WorkspaceID]
		if !ok {
			env = &Environment{
				WorkspaceID:   labels.WorkspaceID,
				Name:          labels.WorkspaceName,
				WorkspacePath: labels.WorkspacePath,
				BuildMethod:   labels.BuildMethod,
			}
			byID[labels.WorkspaceID] = env
		}
		env.Containers = append(env.Containers, EnvironmentContainer{
			ID: c.ID, Name: c.Name, Status: c.State, IsPrimary: labels.IsPrimary, CreatedAt: c.Created,
		})
		// The environment is as old as its oldest container.
		if !c.Created.IsZero() && (env.CreatedAt.IsZero() || c.Created.Before(env.CreatedAt)) {
			env.CreatedAt = c.Created
		}
	}

	envs := make([]Environment, 0, len(byID))
	for _, env := range byID {
		env.State, _, _ = s.stateManager.State(ctx, env.WorkspaceID)
		envs = append(envs, *env)
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].WorkspacePath < envs[j].WorkspacePath })
	return envs, nil
}

// sshStatus collects the SSH endpoint of a workspace's container, best
// effort: what cannot be read is left empty.
func (s *DevContainerService) sshStatus(ctx context.Context, workspaceID string, ci *state.ContainerInfo) *SSHStatus {
	st := &SSHStatus{AllowedIPs: []string{"127.0.0.0/8", "::1/128"}}
	if port, err := container.SSHHostPort(ctx, s.engine, ci); err == nil {
		access := container.RecordedSSHAccess(ci)
		st.Port, st.BindAddress = port, access.BindHost()
		st.AllowedIPs = append(st.AllowedIPs, access.AllowedCIDRs...)
	}
	if path, signer, err := dcxssh.LoadHostKey(workspaceID); err == nil {
		st.HostKeyFile, st.HostKeyFingerprint = path, dcxssh.Fingerprint(signer)
	}
	st.KnownHostsPinned, _ = dcxssh.HasHost(workspaceID)
	return st
}
