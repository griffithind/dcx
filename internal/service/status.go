package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/griffithind/dcx/internal/devcontainer"
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
	Shortcuts  int

	// Filled in only when detailed output was requested.
	Image     string
	StartedAt string
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
		r.State, r.Container, err = s.stateManager.StateWithHash(ctx, ids.WorkspaceID, resolved.ConfigHash)
	} else {
		if cfg, _, lerr := devcontainer.Load(s.workspacePath, s.configPath); lerr == nil {
			r.Config = cfg
		}
		r.State, r.Container, err = s.stateManager.State(ctx, ids.WorkspaceID)
	}
	if err != nil {
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
