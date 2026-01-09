package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// Container represents a Docker container.
type Container struct {
	ID      string
	Name    string
	Image   string
	Status  string
	State   string
	Labels  map[string]string
	Created time.Time
	Running bool
}

// ListContainers returns containers matching the given label filters.
func (c *Client) ListContainers(ctx context.Context, labelFilters map[string]string) ([]Container, error) {
	filterArgs := filters.NewArgs()
	for key, value := range labelFilters {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", key, value))
	}

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]Container, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = ctr.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		result = append(result, Container{
			ID:      ctr.ID,
			Name:    name,
			Image:   ctr.Image,
			Status:  ctr.Status,
			State:   ctr.State,
			Labels:  ctr.Labels,
			Created: time.Unix(ctr.Created, 0),
			Running: ctr.State == "running",
		})
	}

	return result, nil
}

// InspectContainer returns detailed information about a container.
func (c *Client) InspectContainer(ctx context.Context, containerID string) (*Container, error) {
	info, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	name := info.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	created, _ := time.Parse(time.RFC3339Nano, info.Created)

	return &Container{
		ID:      info.ID,
		Name:    name,
		Image:   info.Config.Image,
		Status:  info.State.Status,
		State:   info.State.Status,
		Labels:  info.Config.Labels,
		Created: created,
		Running: info.State.Running,
	}, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	return c.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a running container.
func (c *Client) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	var timeoutSecs *int
	if timeout != nil {
		secs := int(timeout.Seconds())
		timeoutSecs = &secs
	}
	return c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: timeoutSecs})
}

// RemoveContainer removes a container.
// If removeVolumes is true, anonymous volumes attached to the container are also removed.
func (c *Client) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
	})
}

// KillContainer sends a signal to a container.
func (c *Client) KillContainer(ctx context.Context, containerID, signal string) error {
	return c.cli.ContainerKill(ctx, containerID, signal)
}
