package devcontainer

import (
	"os"
	"path/filepath"
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

func TestResolveContainerEnvRefs(t *testing.T) {
	got := ResolveContainerEnvRefs(map[string]string{
		"PATH":    "${containerEnv:PATH}:/custom/bin",
		"MISSING": "${containerEnv:NOPE:fallback}",
		"EMPTY":   "x${containerEnv:NOPE}y",
		"PLAIN":   "keep ${SHELL_VAR} as-is",
	}, map[string]string{"PATH": "/usr/local/bin:/usr/bin"})

	want := map[string]string{
		"PATH":    "/usr/local/bin:/usr/bin:/custom/bin",
		"MISSING": "fallback",
		"EMPTY":   "xy",
		"PLAIN":   "keep ${SHELL_VAR} as-is",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// Load must substitute identically whether the config is auto-discovered or
// passed explicitly with --config.
func TestLoadSubstitutesWithExplicitConfigPath(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer", "variant")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgJSON := `{"image": "alpine:${localEnv:DCX_TEST_TAG:3.20}", "workspaceFolder": "/src/${localWorkspaceFolderBasename}"}`
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(ws, ".devcontainer/variant/devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Image != "alpine:3.20" {
		t.Errorf("image = %q", cfg.Image)
	}
	if cfg.WorkspaceFolder != "/src/"+filepath.Base(ws) {
		t.Errorf("workspaceFolder = %q", cfg.WorkspaceFolder)
	}
}
