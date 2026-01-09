package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		json := `{
			"name": "Test Dev Container",
			"image": "alpine:latest",
			"workspaceFolder": "/workspace"
		}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		assert.Equal(t, "Test Dev Container", cfg.Name)
		assert.Equal(t, "alpine:latest", cfg.Image)
		assert.Equal(t, "/workspace", cfg.WorkspaceFolder)
	})

	t.Run("JSON with comments", func(t *testing.T) {
		json := `{
			// This is a comment
			"name": "Test",
			"image": "alpine:latest"
			/* Block comment */
		}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		assert.Equal(t, "Test", cfg.Name)
		assert.Equal(t, "alpine:latest", cfg.Image)
	})

	t.Run("JSON with trailing comma", func(t *testing.T) {
		json := `{
			"name": "Test",
			"image": "alpine:latest",
		}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		assert.Equal(t, "Test", cfg.Name)
	})
}

func TestPlanType(t *testing.T) {
	t.Run("image-based config", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Image: "alpine:latest",
		}
		assert.Equal(t, PlanTypeImage, cfg.PlanType())
	})

	t.Run("dockerfile-based config", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Build: &BuildConfig{
				Dockerfile: "Dockerfile",
			},
		}
		assert.Equal(t, PlanTypeDockerfile, cfg.PlanType())
	})

	t.Run("compose-based config with string", func(t *testing.T) {
		cfg := &DevContainerConfig{
			DockerComposeFile: "docker-compose.yml",
			Service:           "app",
		}
		assert.Equal(t, PlanTypeCompose, cfg.PlanType())
	})

	t.Run("compose-based config with array", func(t *testing.T) {
		cfg := &DevContainerConfig{
			DockerComposeFile: []interface{}{"docker-compose.yml", "docker-compose.dev.yml"},
			Service:           "app",
		}
		assert.Equal(t, PlanTypeCompose, cfg.PlanType())
	})

	t.Run("compose takes precedence", func(t *testing.T) {
		cfg := &DevContainerConfig{
			Image:             "alpine:latest",
			DockerComposeFile: "docker-compose.yml",
			Service:           "app",
		}
		assert.Equal(t, PlanTypeCompose, cfg.PlanType())
	})
}

func TestMountUnmarshal(t *testing.T) {
	t.Run("string format bind mount", func(t *testing.T) {
		json := `{"mounts": ["source=/host/path,target=/container/path,type=bind"]}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		require.Len(t, cfg.Mounts, 1)
		assert.Equal(t, "/host/path", cfg.Mounts[0].Source)
		assert.Equal(t, "/container/path", cfg.Mounts[0].Target)
		assert.Equal(t, "bind", cfg.Mounts[0].Type)
	})

	t.Run("object format mount", func(t *testing.T) {
		json := `{"mounts": [{"source": "/host/path", "target": "/container/path", "type": "bind"}]}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		require.Len(t, cfg.Mounts, 1)
		assert.Equal(t, "/host/path", cfg.Mounts[0].Source)
		assert.Equal(t, "/container/path", cfg.Mounts[0].Target)
		assert.Equal(t, "bind", cfg.Mounts[0].Type)
	})

	t.Run("docker short format", func(t *testing.T) {
		json := `{"mounts": ["/var/run/docker.sock:/var/run/docker.sock"]}`

		cfg, err := Parse([]byte(json))
		require.NoError(t, err)
		require.Len(t, cfg.Mounts, 1)
		assert.Equal(t, "/var/run/docker.sock", cfg.Mounts[0].Source)
		assert.Equal(t, "/var/run/docker.sock", cfg.Mounts[0].Target)
	})
}

func TestGetForwardPorts(t *testing.T) {
	t.Run("integer ports", func(t *testing.T) {
		cfg := &DevContainerConfig{
			ForwardPorts: []interface{}{float64(8080), float64(3000)},
		}
		ports := cfg.GetForwardPorts()
		assert.Equal(t, []string{"8080:8080", "3000:3000"}, ports)
	})

	t.Run("string ports", func(t *testing.T) {
		cfg := &DevContainerConfig{
			ForwardPorts: []interface{}{"8080", "9000:9000"},
		}
		ports := cfg.GetForwardPorts()
		assert.Equal(t, []string{"8080", "9000:9000"}, ports)
	})

	t.Run("mixed ports", func(t *testing.T) {
		cfg := &DevContainerConfig{
			ForwardPorts: []interface{}{float64(8080), "9000:9000"},
		}
		ports := cfg.GetForwardPorts()
		assert.Equal(t, []string{"8080:8080", "9000:9000"}, ports)
	})

	t.Run("empty ports", func(t *testing.T) {
		cfg := &DevContainerConfig{}
		ports := cfg.GetForwardPorts()
		assert.Nil(t, ports)
	})
}

func TestGetDockerComposeFiles(t *testing.T) {
	t.Run("single string", func(t *testing.T) {
		cfg := &DevContainerConfig{
			DockerComposeFile: "docker-compose.yml",
		}
		files := cfg.GetDockerComposeFiles()
		assert.Equal(t, []string{"docker-compose.yml"}, files)
	})

	t.Run("array of strings", func(t *testing.T) {
		cfg := &DevContainerConfig{
			DockerComposeFile: []interface{}{"docker-compose.yml", "docker-compose.dev.yml"},
		}
		files := cfg.GetDockerComposeFiles()
		assert.Equal(t, []string{"docker-compose.yml", "docker-compose.dev.yml"}, files)
	})

	t.Run("nil returns nil", func(t *testing.T) {
		cfg := &DevContainerConfig{}
		files := cfg.GetDockerComposeFiles()
		assert.Nil(t, files)
	})
}
