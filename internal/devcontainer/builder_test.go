package devcontainer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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

		builder := NewBuilder(slog.Default(), nil)
		resolved, err := builder.Build(context.Background(), BuilderOptions{
			ConfigPath:    "/tmp/test/.devcontainer/devcontainer.json",
			WorkspaceRoot: "/tmp/test",
			Config:        cfg,
		})

		require.NoError(t, err)
		assert.NotEmpty(t, resolved.WorkspaceID)
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

		builder := NewBuilder(slog.Default(), nil)
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

		builder := NewBuilder(slog.Default(), nil)
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
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				builder := NewBuilder(slog.Default(), nil)
				resolved, err := builder.Build(context.Background(), BuilderOptions{
					ConfigPath:    "/tmp/test/devcontainer.json",
					WorkspaceRoot: "/tmp/test",
					Config:        tt.cfg,
				})

				require.NoError(t, err)
				assert.Equal(t, tt.planType, resolved.Plan.Type())
			})
		}

		// Compose plan needs real files on disk for hash computation
		t.Run("compose plan", func(t *testing.T) {
			dir := t.TempDir()
			composeFile := filepath.Join(dir, "docker-compose.yml")
			require.NoError(t, os.WriteFile(composeFile, []byte("services:\n  app:\n    image: alpine\n"), 0644))

			cfg := &DevContainerConfig{DockerComposeFile: StringOrSlice{"docker-compose.yml"}, Service: "app"}
			builder := NewBuilder(slog.Default(), nil)
			resolved, err := builder.Build(context.Background(), BuilderOptions{
				ConfigPath:    filepath.Join(dir, "devcontainer.json"),
				WorkspaceRoot: dir,
				Config:        cfg,
			})

			require.NoError(t, err)
			assert.Equal(t, PlanTypeCompose, resolved.Plan.Type())
		})
	})
}

func TestParseGPURequirementsOptional(t *testing.T) {
	opt := parseGPURequirements(&HostRequirements{GPU: "optional"})
	if opt == nil || !opt.Enabled || !opt.Optional {
		t.Fatalf("optional: %+v", opt)
	}
	req := parseGPURequirements(&HostRequirements{GPU: true})
	if req == nil || !req.Enabled || req.Optional {
		t.Fatalf("required: %+v", req)
	}
	if empty := parseGPURequirements(&HostRequirements{GPU: map[string]interface{}{}}); empty == nil || empty.Enabled {
		t.Fatalf("an empty object states no requirement: %+v", empty)
	}
}

func TestOverrideCommandDefault(t *testing.T) {
	// Per devcontainer spec:
	// - Default true for image/dockerfile-based containers
	// - Default false for compose-based containers
	tests := []struct {
		name            string
		plan            ExecutionPlan
		overrideCommand *bool
		wantOverride    bool
	}{
		{
			name:            "image plan - default should override",
			plan:            NewImagePlan("alpine:latest"),
			overrideCommand: nil,
			wantOverride:    true,
		},
		{
			name:            "image plan - explicit true",
			plan:            NewImagePlan("alpine:latest"),
			overrideCommand: overridePtr(true),
			wantOverride:    true,
		},
		{
			name:            "image plan - explicit false",
			plan:            NewImagePlan("alpine:latest"),
			overrideCommand: overridePtr(false),
			wantOverride:    false,
		},
		{
			name:            "dockerfile plan - default should override",
			plan:            NewDockerfilePlan("Dockerfile", "."),
			overrideCommand: nil,
			wantOverride:    true,
		},
		{
			name:            "dockerfile plan - explicit false",
			plan:            NewDockerfilePlan("Dockerfile", "."),
			overrideCommand: overridePtr(false),
			wantOverride:    false,
		},
		{
			name:            "compose plan - default should NOT override",
			plan:            NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: nil,
			wantOverride:    false,
		},
		{
			name:            "compose plan - explicit true",
			plan:            NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: overridePtr(true),
			wantOverride:    true,
		},
		{
			name:            "compose plan - explicit false",
			plan:            NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: overridePtr(false),
			wantOverride:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOverride, overrideCommand(tt.plan.Type(), tt.overrideCommand), "overrideCommand default for %T", tt.plan)
		})
	}
}

func overridePtr(b bool) *bool { return &b }
