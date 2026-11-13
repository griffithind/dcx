package service

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/state"
)

// StopOutcome reports what Stop did.
type StopOutcome int

const (
	// StopNotFound means the workspace has no devcontainer.
	StopNotFound StopOutcome = iota
	// StopAlreadyStopped means the container was not running.
	StopAlreadyStopped
	// StopSkipped means shutdownAction "none" kept it running (no --force).
	StopSkipped
	// StopDone means the container was stopped.
	StopDone
)

// StopOptions configures Stop and Restart.
type StopOptions struct {
	// Force stops even when shutdownAction is "none".
	Force bool
}

// Stop stops the workspace's containers without removing them, honouring
// shutdownAction "none" unless forced.
func (s *DevContainerService) Stop(ctx context.Context, opts StopOptions) (StopOutcome, error) {
	ids, _ := s.GetIdentifiers()
	st, info, err := s.stateManager.GetState(ctx, ids.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("failed to get state: %w", err)
	}
	switch {
	case st == state.StateAbsent:
		return StopNotFound, nil
	case info == nil || !info.Running:
		return StopAlreadyStopped, nil
	case !opts.Force && s.keepRunning():
		return StopSkipped, nil
	}
	if err := s.stopContainers(ctx, info, ids.ProjectName); err != nil {
		return 0, err
	}
	return StopDone, nil
}

// RestartOptions configures Restart.
type RestartOptions struct {
	StopOptions
	// Rebuild recreates the container with freshly built images instead of
	// stopping and starting it.
	Rebuild bool
}

// Restart stops the workspace's containers and brings them up again
// through Up, so the full start sequence runs (agent, secrets, environment,
// postStart hooks, SSH config). It reports whether shutdownAction "none"
// skipped it.
func (s *DevContainerService) Restart(ctx context.Context, opts RestartOptions) (skipped bool, err error) {
	ids, _ := s.GetIdentifiers()
	st, info, err := s.stateManager.GetState(ctx, ids.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("failed to get state: %w", err)
	}
	if st == state.StateAbsent {
		return false, fmt.Errorf("no devcontainer found, use 'dcx up' to create one")
	}
	if !opts.Force && s.keepRunning() {
		return true, nil
	}
	if opts.Rebuild {
		return false, s.Up(ctx, UpOptions{Recreate: true, Rebuild: true})
	}
	if info != nil && info.Running {
		s.report.Infof("Stopping devcontainer...")
		if err := s.stopContainers(ctx, info, ids.ProjectName); err != nil {
			return false, err
		}
	}
	return false, s.Up(ctx, UpOptions{})
}

// keepRunning reports whether devcontainer.json sets shutdownAction "none".
// It reads the file directly (no feature resolution), so stopping works
// offline.
func (s *DevContainerService) keepRunning() bool {
	cfg, _, err := devcontainer.Load(s.workspacePath, s.configPath)
	return err == nil && cfg.ShutdownAction == "none"
}

// stopContainers stops a workspace's container (single) or compose project.
func (s *DevContainerService) stopContainers(ctx context.Context, info *state.ContainerInfo, projectName string) error {
	if info.IsSingleContainer() {
		if err := s.engine.StopContainer(ctx, info.ID, nil); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		return nil
	}
	project := container.ExistingComposeProject(info, projectName, s.workspacePath)
	if err := project.Stop(ctx, s.engine); err != nil {
		return fmt.Errorf("failed to stop containers: %w", err)
	}
	return nil
}
