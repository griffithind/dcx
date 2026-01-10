package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetermineContainerWorkspaceFolder(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *DevContainerConfig
		localWorkspace string
		want           string
	}{
		{
			name: "explicit workspaceFolder takes precedence",
			cfg: &DevContainerConfig{
				WorkspaceFolder: "/custom/path",
				Image:           "alpine",
			},
			localWorkspace: "/home/user/project",
			want:           "/custom/path",
		},
		{
			name: "image plan defaults to /workspaces/<basename>",
			cfg: &DevContainerConfig{
				Image: "alpine",
			},
			localWorkspace: "/home/user/my-project",
			want:           "/workspaces/my-project",
		},
		{
			name: "dockerfile plan defaults to /workspaces/<basename>",
			cfg: &DevContainerConfig{
				Build: &BuildConfig{
					Dockerfile: "Dockerfile",
				},
			},
			localWorkspace: "/home/user/my-app",
			want:           "/workspaces/my-app",
		},
		{
			name: "compose plan defaults to / (per spec)",
			cfg: &DevContainerConfig{
				DockerComposeFile: []string{"docker-compose.yml"},
				Service:           "app",
			},
			localWorkspace: "/home/user/compose-project",
			want:           "/",
		},
		{
			name: "compose with explicit workspaceFolder",
			cfg: &DevContainerConfig{
				DockerComposeFile: []string{"docker-compose.yml"},
				Service:           "app",
				WorkspaceFolder:   "/workspace",
			},
			localWorkspace: "/home/user/project",
			want:           "/workspace",
		},
		{
			name: "handles workspace with trailing slash",
			cfg: &DevContainerConfig{
				Image: "alpine",
			},
			localWorkspace: "/home/user/project/",
			want:           "/workspaces/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineContainerWorkspaceFolder(tt.cfg, tt.localWorkspace)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ctx   *SubstitutionContext
		want  string
	}{
		{
			name:  "no substitution needed",
			input: "plain text",
			ctx:   &SubstitutionContext{},
			want:  "plain text",
		},
		{
			name:  "localWorkspaceFolder",
			input: "${localWorkspaceFolder}/src",
			ctx: &SubstitutionContext{
				LocalWorkspaceFolder: "/home/user/project",
			},
			want: "/home/user/project/src",
		},
		{
			name:  "containerWorkspaceFolder",
			input: "${containerWorkspaceFolder}/build",
			ctx: &SubstitutionContext{
				ContainerWorkspaceFolder: "/workspaces/project",
			},
			want: "/workspaces/project/build",
		},
		{
			name:  "localEnv with value",
			input: "${localEnv:HOME}/test",
			ctx: &SubstitutionContext{
				LocalEnv: func(key string) string {
					if key == "HOME" {
						return "/home/testuser"
					}
					return ""
				},
			},
			want: "/home/testuser/test",
		},
		{
			name:  "localEnv with default",
			input: "${localEnv:MISSING:defaultvalue}",
			ctx: &SubstitutionContext{
				LocalEnv: func(key string) string { return "" },
			},
			want: "defaultvalue",
		},
		{
			name:  "containerEnv",
			input: "path=${containerEnv:PATH}",
			ctx: &SubstitutionContext{
				ContainerEnv: map[string]string{"PATH": "/usr/bin"},
			},
			want: "path=/usr/bin",
		},
		{
			name:  "multiple substitutions",
			input: "${localWorkspaceFolder}:${containerWorkspaceFolder}",
			ctx: &SubstitutionContext{
				LocalWorkspaceFolder:     "/local",
				ContainerWorkspaceFolder: "/container",
			},
			want: "/local:/container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.input, tt.ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}
