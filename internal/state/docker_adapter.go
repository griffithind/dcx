package state

import (
	"context"
	"time"

	"github.com/griffithind/dcx/internal/docker"
)

// DockerClientAdapter adapts docker.Client to implement ContainerClient.
// This allows creating a StateManager directly from a docker.Client.
type DockerClientAdapter struct {
	client *docker.Client
}

// NewDockerClientAdapter creates an adapter that wraps docker.Client.
func NewDockerClientAdapter(client *docker.Client) *DockerClientAdapter {
	return &DockerClientAdapter{client: client}
}

// NewStateManagerForDocker creates a StateManager using a docker.Client.
// This is a convenience function for CLI commands that already have a docker.Client.
func NewStateManagerForDocker(client *docker.Client) *StateManager {
	return NewStateManager(NewDockerClientAdapter(client))
}

func (a *DockerClientAdapter) ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]ContainerSummary, error) {
	containers, err := a.client.ListContainers(ctx, labels)
	if err != nil {
		return nil, err
	}

	result := make([]ContainerSummary, len(containers))
	for i, c := range containers {
		result[i] = ContainerSummary{
			ID:      c.ID,
			Name:    c.Name,
			State:   c.State,
			Running: c.Running,
			Labels:  c.Labels,
		}
	}
	return result, nil
}

func (a *DockerClientAdapter) InspectContainer(ctx context.Context, containerID string) (*ContainerDetails, error) {
	c, err := a.client.InspectContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	return &ContainerDetails{
		ID:      c.ID,
		Name:    c.Name,
		State:   c.State,
		Running: c.Running,
		Labels:  c.Labels,
	}, nil
}

func (a *DockerClientAdapter) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	return a.client.StopContainer(ctx, containerID, timeout)
}

func (a *DockerClientAdapter) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	return a.client.RemoveContainer(ctx, containerID, force, removeVolumes)
}

// Verify the adapter implements the interface
var _ ContainerClient = (*DockerClientAdapter)(nil)
