package devcontainer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderBuild(t *testing.T) {
	t.Run("creates ResolvedDevContainer with correct identity", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Name:            "Test Container",
			Image:           "alpine:latest",
			WorkspaceFolder: "/workspace",
		}

		builder := NewBuilder(slog.Default())
		resolved, err := builder.Build(context.Background(), BuilderOptions{
			ConfigPath:    "/tmp/test/.devcontainer/devcontainer.json",
			WorkspaceRoot: "/tmp/test",
			Config:        cfg,
		})

		require.NoError(t, err)
		assert.NotEmpty(t, resolved.ID)
		assert.Equal(t, "Test Container", resolved.Name)
		assert.Equal(t, "/tmp/test/.devcontainer/devcontainer.json", resolved.ConfigPath)
		assert.Equal(t, "/tmp/test", resolved.LocalRoot)
		assert.Equal(t, "/workspace", resolved.WorkspaceFolder)
	})

	t.Run("populates ForwardPorts correctly", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Image:        "alpine:latest",
			ForwardPorts: []interface{}{float64(8080), "9000:9000"},
		}

		builder := NewBuilder(slog.Default())
		resolved, err := builder.Build(context.Background(), BuilderOptions{
			ConfigPath:    "/tmp/test/devcontainer.json",
			WorkspaceRoot: "/tmp/test",
			Config:        cfg,
		})

		require.NoError(t, err)
		require.Len(t, resolved.ForwardPorts, 2)
		assert.Equal(t, 8080, resolved.ForwardPorts[0].ContainerPort)
		assert.Equal(t, 8080, resolved.ForwardPorts[0].HostPort)
		assert.Equal(t, 9000, resolved.ForwardPorts[1].ContainerPort)
		assert.Equal(t, 9000, resolved.ForwardPorts[1].HostPort)
	})

	t.Run("parses Mounts correctly", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Image: "alpine:latest",
			Mounts: []Mount{
				{Source: "/host/path", Target: "/container/path", Type: "bind"},
				{Source: "myvolume", Target: "/data", Type: "volume"},
			},
		}

		builder := NewBuilder(slog.Default())
		resolved, err := builder.Build(context.Background(), BuilderOptions{
			ConfigPath:    "/tmp/test/devcontainer.json",
			WorkspaceRoot: "/tmp/test",
			Config:        cfg,
		})

		require.NoError(t, err)
		require.Len(t, resolved.Mounts, 2)
		assert.Equal(t, "bind", resolved.Mounts[0].Type)
		assert.Equal(t, "/host/path", resolved.Mounts[0].Source)
		assert.Equal(t, "/container/path", resolved.Mounts[0].Target)
		assert.Equal(t, "volume", resolved.Mounts[1].Type)
	})

	t.Run("creates correct plan type", func(t *testing.T) {
		tests := []struct {
			name     string
			cfg      *DevContainerConfig
			planType PlanType
		}{
			{
				name:     "image plan",
				cfg:      &DevContainerConfig{Image: "alpine:latest"},
				planType: PlanTypeImage,
			},
			{
				name:     "dockerfile plan",
				cfg:      &DevContainerConfig{Build: &BuildConfig{Dockerfile: "Dockerfile"}},
				planType: PlanTypeDockerfile,
			},
			{
				name:     "compose plan",
				cfg:      &DevContainerConfig{DockerComposeFile: "docker-compose.yml", Service: "app"},
				planType: PlanTypeCompose,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				builder := NewBuilder(slog.Default())
				resolved, err := builder.Build(context.Background(), BuilderOptions{
					ConfigPath:    "/tmp/test/devcontainer.json",
					WorkspaceRoot: "/tmp/test",
					Config:        tt.cfg,
				})

				require.NoError(t, err)
				assert.Equal(t, tt.planType, resolved.Plan.Type())
			})
		}
	})

	t.Run("uses project name when provided", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Name:  "Config Name",
			Image: "alpine:latest",
		}

		builder := NewBuilder(slog.Default())
		resolved, err := builder.Build(context.Background(), BuilderOptions{
			ConfigPath:    "/tmp/test/devcontainer.json",
			WorkspaceRoot: "/tmp/test",
			Config:        cfg,
			ProjectName:   "custom-project",
		})

		require.NoError(t, err)
		assert.Equal(t, "custom-project", resolved.Name)
	})
}
