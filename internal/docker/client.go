// Package docker provides a wrapper around the Docker Engine API client.
package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
)

// Client wraps the Docker client with dcx-specific functionality.
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client.
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{cli: cli}, nil
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.cli.Close()
}

// APIClient returns the underlying Docker API client.
func (c *Client) APIClient() *client.Client {
	return c.cli
}

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Docker requires lowercase alphanumeric with hyphens/underscores, starting with letter.
func SanitizeProjectName(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with underscores and filter invalid characters
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if r == ' ' {
			result.WriteRune('_')
		}
		// Skip other characters
	}

	sanitized := result.String()
	if sanitized == "" {
		return ""
	}

	// Ensure starts with a letter (Docker requirement)
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "dcx_" + sanitized
	}

	return sanitized
}

// Ping checks if the Docker daemon is accessible.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ServerVersion returns the Docker server version.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	version, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	return version.Version, nil
}

// SystemInfo contains information about the Docker daemon's resources.
type SystemInfo struct {
	NCPU         int    // Number of CPUs available to Docker
	MemTotal     uint64 // Total memory available to Docker in bytes
	OSType       string // Operating system type (linux, windows)
	Architecture string // Architecture (x86_64, arm64, etc.)
}

// Info returns system-wide information about Docker.
// This reflects Docker's configured resource limits, which may be less than the host's
// actual resources (e.g., Docker Desktop VM limits, cgroup limits).
func (c *Client) Info(ctx context.Context) (*SystemInfo, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker info: %w", err)
	}

	return &SystemInfo{
		NCPU:         info.NCPU,
		MemTotal:     uint64(info.MemTotal),
		OSType:       info.OSType,
		Architecture: info.Architecture,
	}, nil
}
